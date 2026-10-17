package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

// Retention rules for the nightly cleanup. Task 013 has the reasons.
const (
	SessionsKept         = 7 * 24 * time.Hour  // after expiry or logout
	TicketsKept          = time.Hour           // after expiry
	InvitesKept          = 30 * 24 * time.Hour // after expiry
	PixelsKept           = 30 * 24 * time.Hour // the owner's call; the board itself is never touched
	TimelapsesKept       = 10                  // per canvas, what the list shows
	FailedTimelapsesKept = 7 * 24 * time.Hour
	PendingTemplateKept  = 24 * time.Hour
	pixelDeleteBatch     = 5000
)

// CleanupCounts is what one run deleted, or with a dry run would delete.
type CleanupCounts struct {
	Sessions, Tickets, Invites, Pixels, Timelapses, Templates int64
}

type Cleanup struct {
	pool    *pgxpool.Pool
	objects ObjectStore // nil: timelapses and templates are skipped, their files cannot be deleted
}

func NewCleanup(pool *pgxpool.Pool, objects ObjectStore) *Cleanup {
	return &Cleanup{pool: pool, objects: objects}
}

// Run applies every rule as of now. One rule failing does not stop the others; their
// errors are returned together. Safe to run twice: the second run finds nothing.
func (c *Cleanup) Run(ctx context.Context, now time.Time, dryRun bool) (CleanupCounts, error) {
	var (
		n    CleanupCounts
		errs []error
	)
	q := store.New(c.pool)
	rule := func(name string, count, del func(context.Context, time.Time) (int64, error), before time.Time, into *int64) {
		f := del
		if dryRun {
			f = count
		}
		v, err := f(ctx, before)
		*into = v
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	rule("sessions", q.CountOldSessions, q.DeleteOldSessions, now.Add(-SessionsKept), &n.Sessions)
	rule("ws_tickets", q.CountOldWSTickets, q.DeleteOldWSTickets, now.Add(-TicketsKept), &n.Tickets)
	rule("invites", q.CountOldInvites, q.DeleteOldInvites, now.Add(-InvitesKept), &n.Invites)

	// Across tenants: spaces has no RLS; everything else is read inside each space's tenant.
	spaces, err := q.ListSpaceIDs(ctx)
	if err != nil {
		return n, errors.Join(append(errs, fmt.Errorf("list spaces: %w", err))...)
	}
	for _, spaceID := range spaces {
		var canvases []uuid.UUID
		err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
			var err error
			canvases, err = q.ListCanvasIDs(ctx, spaceID)
			return err
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("space %s: list canvases: %w", spaceID, err))
			continue
		}
		for _, canvasID := range canvases {
			p, err := c.pixels(ctx, spaceID, canvasID, now.Add(-PixelsKept), dryRun)
			n.Pixels += p
			if err != nil {
				errs = append(errs, fmt.Errorf("canvas %s: pixels: %w", canvasID, err))
			}
			if c.objects == nil {
				continue
			}
			t, err := c.timelapses(ctx, spaceID, canvasID, now.Add(-FailedTimelapsesKept), dryRun)
			n.Timelapses += t
			if err != nil {
				errs = append(errs, fmt.Errorf("canvas %s: timelapses: %w", canvasID, err))
			}
		}
		if c.objects == nil {
			continue
		}
		t, err := c.templates(ctx, spaceID, now.Add(-PendingTemplateKept), dryRun)
		n.Templates += t
		if err != nil {
			errs = append(errs, fmt.Errorf("space %s: templates: %w", spaceID, err))
		}
	}
	return n, errors.Join(errs...)
}

// pixels deletes in batches, one short transaction each, so players placing pixels never
// wait long on a lock.
func (c *Cleanup) pixels(ctx context.Context, spaceID, canvasID uuid.UUID, before time.Time, dryRun bool) (int64, error) {
	var total int64
	for {
		var n int64
		err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
			var err error
			if dryRun {
				n, err = q.CountOldPixels(ctx, store.CountOldPixelsParams{CanvasID: canvasID, SpaceID: spaceID, Before: before})
			} else {
				n, err = q.DeleteOldPixels(ctx, store.DeleteOldPixelsParams{
					CanvasID: canvasID, SpaceID: spaceID, Before: before, Batch: pixelDeleteBatch,
				})
			}
			return err
		})
		total += n
		if err != nil || dryRun || n < pixelDeleteBatch {
			return total, err
		}
	}
}

// timelapses deletes each GIF before its row: a failed file delete keeps the row for tomorrow.
func (c *Cleanup) timelapses(ctx context.Context, spaceID, canvasID uuid.UUID, failedBefore time.Time, dryRun bool) (int64, error) {
	var rows []store.ListOldTimelapsesRow
	err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
		var err error
		rows, err = q.ListOldTimelapses(ctx, store.ListOldTimelapsesParams{
			CanvasID: canvasID, SpaceID: spaceID, KeptRows: TimelapsesKept, FailedBefore: failedBefore,
		})
		return err
	})
	if err != nil || dryRun {
		return int64(len(rows)), err
	}
	var n int64
	for _, r := range rows {
		if r.ObjectKey.Valid {
			if err := c.objects.Delete(ctx, r.ObjectKey.String); err != nil {
				return n, err
			}
		}
		err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
			return q.DeleteTimelapse(ctx, store.DeleteTimelapseParams{ID: r.ID, SpaceID: spaceID})
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// templates removes uploads that never completed, file first, then row.
func (c *Cleanup) templates(ctx context.Context, spaceID uuid.UUID, before time.Time, dryRun bool) (int64, error) {
	var rows []store.ListStalePendingTemplatesRow
	err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
		var err error
		rows, err = q.ListStalePendingTemplates(ctx, store.ListStalePendingTemplatesParams{SpaceID: spaceID, Before: before})
		return err
	})
	if err != nil || dryRun {
		return int64(len(rows)), err
	}
	var n int64
	for _, r := range rows {
		if err := c.objects.Delete(ctx, r.ObjectKey); err != nil {
			return n, err
		}
		err := inTenant(ctx, c.pool, spaceID, func(q *store.Queries) error {
			return q.DeleteTemplate(ctx, store.DeleteTemplateParams{ID: r.ID, SpaceID: spaceID})
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
