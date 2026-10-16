package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	snapshotsListed     = 48 // two days of hours
	snapshotDownloadTTL = time.Hour
)

var ErrSnapshotsOff = errors.New("snapshots are not configured")

type Snapshot struct {
	ID           uuid.UUID
	TakenAt      time.Time
	URL          string
	URLExpiresAt time.Time
}

// SnapshotPass counts what one pass did, for its log line.
type SnapshotPass struct {
	Canvases, Rendered, Skipped, Failed int
}

type Snapshots struct {
	pool    *pgxpool.Pool
	objects ObjectStore // nil when the bucket is not configured
	queue   Queue       // nil: the API renders the snapshots itself
}

func NewSnapshots(pool *pgxpool.Pool, objects ObjectStore) *Snapshots {
	return &Snapshots{pool: pool, objects: objects}
}

// WithQueue hands the hourly pass to the worker.
func (s *Snapshots) WithQueue(q Queue) *Snapshots {
	s.queue = q
	return s
}

// SnapshotKey is the only place a snapshot's object key is built, from server values only.
func SnapshotKey(spaceID, canvasID uuid.UUID, hour time.Time) string {
	// No colons in the name (2026-10-15T16Z): they are escaped in URLs and refused by some tools.
	return fmt.Sprintf("spaces/%s/snapshots/%s/%s.png", spaceID, canvasID, hour.UTC().Format("2006-01-02T15Z"))
}

// Run fires at the top of every hour (UTC) until ctx ends: it queues the pass for the
// worker, or takes it here when the queue is unavailable. Step 13 moves the timer to a
// cron service, and it must go before a second replica exists.
func (s *Snapshots) Run(ctx context.Context) {
	if s.objects == nil {
		return
	}
	for {
		// Waits from the end of the last pass, so a slow pass delays the next instead of overlapping it.
		t := time.NewTimer(time.Until(time.Now().Truncate(time.Hour).Add(time.Hour)))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		start := time.Now()
		if s.queue != nil {
			err := s.queue.Snapshots(ctx, start)
			if err == nil {
				slog.Info("snapshots queued", "hour", start.UTC().Truncate(time.Hour))
				continue
			}
			slog.Warn("snapshots not queued, taking them here", "err", err)
		}
		p := s.Pass(ctx, start)
		slog.Info("snapshots", "canvases", p.Canvases, "rendered", p.Rendered, "skipped", p.Skipped,
			"failed", p.Failed, "ms", time.Since(start).Milliseconds())
	}
}

// Pass snapshots every canvas that changed since its last snapshot, as of now's hour.
// One canvas failing does not stop the others.
func (s *Snapshots) Pass(ctx context.Context, now time.Time) SnapshotPass {
	var p SnapshotPass
	if s.objects == nil {
		return p
	}
	hour := now.UTC().Truncate(time.Hour)

	// Across tenants: spaces has no RLS; each space's canvases are read inside its own tenant.
	spaces, err := store.New(s.pool).ListSpaceIDs(ctx)
	if err != nil {
		slog.Error("snapshots: list spaces", "err", err)
		p.Failed++
		return p
	}
	for _, spaceID := range spaces {
		var canvases []uuid.UUID
		err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
			var err error
			canvases, err = q.ListCanvasIDs(ctx, spaceID)
			return err
		})
		if err != nil {
			slog.Error("snapshots: list canvases", "space_id", spaceID, "err", err)
			p.Failed++
			continue
		}
		for _, canvasID := range canvases {
			p.Canvases++
			took, err := s.snapshot(ctx, spaceID, canvasID, hour)
			switch {
			case err != nil:
				slog.Error("snapshot failed", "space_id", spaceID, "canvas_id", canvasID, "err", err)
				p.Failed++
			case took:
				p.Rendered++
			default:
				p.Skipped++
			}
		}
	}
	return p
}

// snapshot renders and stores one canvas, unless it has no pixels, has not changed, or
// this hour's picture exists. One transaction: a failed upload leaves no row.
func (s *Snapshots) snapshot(ctx context.Context, spaceID, canvasID uuid.UUID, hour time.Time) (bool, error) {
	took := false
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		if err != nil {
			return err
		}
		palette, err := paletteByID(c.Palette)
		if err != nil {
			return err
		}
		cells, err := q.ListCanvasCells(ctx, store.ListCanvasCellsParams{CanvasID: canvasID, SpaceID: spaceID})
		if err != nil || len(cells) == 0 {
			return err
		}
		var newest int64
		for _, cell := range cells {
			newest = max(newest, cell.PixelID)
		}

		last, err := q.LatestSnapshotPixelID(ctx, store.LatestSnapshotPixelIDParams{CanvasID: canvasID, SpaceID: spaceID})
		switch {
		case errors.Is(err, pgx.ErrNoRows): // never snapshotted
		case err != nil:
			return err
		case last == newest:
			return nil
		}

		key := SnapshotKey(spaceID, canvasID, hour)
		n, err := q.ClaimSnapshot(ctx, store.ClaimSnapshotParams{
			SpaceID: spaceID, CanvasID: canvasID, ObjectKey: key, LastPixelID: newest, TakenAt: hour,
		})
		if err != nil || n == 0 {
			return err
		}
		img, err := renderPNG(int(c.Width), int(c.Height), palette, cells)
		if err != nil {
			return err
		}
		if err := s.objects.Put(ctx, key, "image/png", img); err != nil {
			return err
		}
		took = true
		return nil
	})
	return took, err
}

// renderPNG draws one image pixel per cell with the canvas's palette; unset cells are index 0.
func renderPNG(width, height int, palette Palette, cells []store.ListCanvasCellsRow) ([]byte, error) {
	colors := make(color.Palette, len(palette.Colors))
	for i, hex := range palette.Colors {
		var r, g, b uint8
		if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
			return nil, fmt.Errorf("palette %s color %q: %w", palette.ID, hex, err)
		}
		colors[i] = color.RGBA{R: r, G: g, B: b, A: 255}
	}
	img := image.NewPaletted(image.Rect(0, 0, width, height), colors)
	for _, cell := range cells {
		img.SetColorIndex(int(cell.X), int(cell.Y), uint8(cell.Color))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// List returns the canvas's latest snapshots, newest first, with signed download URLs. Live.
func (s *Snapshots) List(ctx context.Context, spaceID, canvasID uuid.UUID) ([]Snapshot, error) {
	if s.objects == nil {
		return nil, ErrSnapshotsOff
	}
	var rows []store.ListSnapshotsRow
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		rows, err = q.ListSnapshots(ctx, store.ListSnapshotsParams{CanvasID: canvasID, SpaceID: spaceID, MaxRows: snapshotsListed})
		return err
	})
	if err != nil {
		return nil, err
	}

	expires := time.Now().Add(snapshotDownloadTTL)
	out := make([]Snapshot, 0, len(rows))
	for _, r := range rows {
		url, err := s.objects.SignDownload(ctx, r.ObjectKey, snapshotDownloadTTL)
		if err != nil {
			return nil, err
		}
		out = append(out, Snapshot{ID: r.ID, TakenAt: r.TakenAt, URL: url, URLExpiresAt: expires})
	}
	return out, nil
}
