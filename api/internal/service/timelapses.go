package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	timelapseMaxFrames   = 240 // ten days of hours
	timelapseFrameDelay  = 25  // hundredths of a second: 4 frames a second
	timelapseStaleAfter  = 10 * time.Minute
	timelapseDownloadTTL = time.Hour
	snapshotMaxBytes     = 1 << 20 // ours are a few hundred bytes; a cap all the same
)

var (
	ErrTimelapsesOff       = errors.New("timelapses are not configured")
	ErrTimelapsePending    = errors.New("a timelapse of this canvas is already being made")
	ErrQueueUnavailable    = errors.New("the job queue is unavailable, try again in a minute")
	errNotEnoughSnapshots  = "not enough snapshots yet: a timelapse needs at least 2 hours with changes"
	errTimelapseBuildFails = "could not build the timelapse"
)

type Timelapse struct {
	ID           uuid.UUID
	Status       string
	CreatedAt    time.Time
	FinishedAt   *time.Time
	Frames       int
	Error        string
	URL          string
	URLExpiresAt time.Time
}

type Timelapses struct {
	pool    *pgxpool.Pool
	objects ObjectStore // nil when the bucket is not configured
	queue   Queue       // nil when there is no Redis
}

func NewTimelapses(pool *pgxpool.Pool, objects ObjectStore, queue Queue) *Timelapses {
	return &Timelapses{pool: pool, objects: objects, queue: queue}
}

// TimelapseKey is built from server values only.
func TimelapseKey(spaceID, canvasID, timelapseID uuid.UUID) string {
	return fmt.Sprintf("spaces/%s/timelapses/%s/%s.gif", spaceID, canvasID, timelapseID)
}

// Request records a pending timelapse, then queues its job. Queued after the commit, so
// the worker can never look for a row it cannot see yet; if queueing fails, the row is
// deleted again and nothing half-made is left behind.
func (s *Timelapses) Request(ctx context.Context, spaceID, canvasID, userID uuid.UUID) (Timelapse, error) {
	switch {
	case s.objects == nil:
		return Timelapse{}, ErrTimelapsesOff
	case s.queue == nil:
		return Timelapse{}, ErrQueueUnavailable
	}
	var t store.Timelapse
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if err := q.FailStaleTimelapses(ctx, store.FailStaleTimelapsesParams{
			CanvasID: canvasID, SpaceID: spaceID, OlderThanSeconds: int32(timelapseStaleAfter.Seconds()),
		}); err != nil {
			return err
		}
		id, err := q.CreateTimelapse(ctx, store.CreateTimelapseParams{
			SpaceID: spaceID, CanvasID: canvasID, RequestedBy: uuid.NullUUID{UUID: userID, Valid: true},
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "timelapses_one_pending" {
			return ErrTimelapsePending
		}
		if err != nil {
			return err
		}
		t, err = q.GetTimelapse(ctx, store.GetTimelapseParams{ID: id, SpaceID: spaceID})
		return err
	})
	if err != nil {
		return Timelapse{}, err
	}
	if err := s.queue.Timelapse(ctx, spaceID, t.ID); err != nil {
		uctx := context.WithoutCancel(ctx)
		undo := inTenant(uctx, s.pool, spaceID, func(q *store.Queries) error {
			return q.DeleteTimelapse(uctx, store.DeleteTimelapseParams{ID: t.ID, SpaceID: spaceID})
		})
		return Timelapse{}, errors.Join(ErrQueueUnavailable, err, undo)
	}
	return toTimelapse(t), nil
}

// List: the canvas's newest 10, live; a ready one's URL lasts an hour.
func (s *Timelapses) List(ctx context.Context, spaceID, canvasID uuid.UUID) ([]Timelapse, error) {
	if s.objects == nil {
		return nil, ErrTimelapsesOff
	}
	var rows []store.Timelapse
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		rows, err = q.ListTimelapses(ctx, store.ListTimelapsesParams{CanvasID: canvasID, SpaceID: spaceID})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Timelapse, 0, len(rows))
	for _, r := range rows {
		t := toTimelapse(r)
		if r.Status == "ready" {
			if t.URL, err = s.objects.SignDownload(ctx, r.ObjectKey.String, timelapseDownloadTTL); err != nil {
				return nil, err
			}
			t.URLExpiresAt = time.Now().Add(timelapseDownloadTTL)
		}
		out = append(out, t)
	}
	return out, nil
}

// Build is the worker's job: the canvas's newest snapshots, oldest first, as one animated
// GIF. Safe to run twice: a timelapse no longer pending is left alone. On the last
// attempt a failure marks it failed, so it never stays pending for good.
func (s *Timelapses) Build(ctx context.Context, spaceID, timelapseID uuid.UUID, lastAttempt bool) error {
	var (
		t    store.Timelapse
		keys []string
	)
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		var err error
		if t, err = q.GetTimelapse(ctx, store.GetTimelapseParams{ID: timelapseID, SpaceID: spaceID}); err != nil {
			return err
		}
		keys, err = q.ListSnapshotKeysForTimelapse(ctx, store.ListSnapshotKeysForTimelapseParams{
			CanvasID: t.CanvasID, SpaceID: spaceID, MaxFrames: timelapseMaxFrames,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.Status != "pending") {
		return nil
	}
	if err != nil {
		return err
	}
	if len(keys) < 2 {
		return s.fail(ctx, spaceID, timelapseID, errNotEnoughSnapshots)
	}

	frames, err := s.renderGIF(ctx, keys)
	if err == nil {
		key := TimelapseKey(spaceID, t.CanvasID, timelapseID)
		if err = s.objects.Put(ctx, key, "image/gif", frames); err == nil {
			err = inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
				_, err := q.MarkTimelapseReady(ctx, store.MarkTimelapseReadyParams{
					ObjectKey: pgtype.Text{String: key, Valid: true}, Frames: pgtype.Int4{Int32: int32(len(keys)), Valid: true},
					ID: timelapseID, SpaceID: spaceID,
				})
				return err
			})
		}
	}
	if err != nil && lastAttempt {
		return errors.Join(err, s.fail(ctx, spaceID, timelapseID, errTimelapseBuildFails))
	}
	return err
}

func (s *Timelapses) renderGIF(ctx context.Context, keys []string) ([]byte, error) {
	anim := &gif.GIF{}
	for _, key := range keys {
		data, err := s.objects.Get(ctx, key, snapshotMaxBytes)
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", key, err)
		}
		frame, ok := img.(*image.Paletted)
		if !ok { // ours are paletted; anything else is mapped onto a fixed palette
			frame = image.NewPaletted(img.Bounds(), gifFallbackPalette)
			draw.Draw(frame, img.Bounds(), img, img.Bounds().Min, draw.Src)
		}
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, timelapseFrameDelay)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var gifFallbackPalette = palette.Plan9

func (s *Timelapses) fail(ctx context.Context, spaceID, timelapseID uuid.UUID, reason string) error {
	return inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		return q.MarkTimelapseFailed(ctx, store.MarkTimelapseFailedParams{
			Reason: pgtype.Text{String: reason, Valid: true}, ID: timelapseID, SpaceID: spaceID,
		})
	})
}

func toTimelapse(r store.Timelapse) Timelapse {
	return Timelapse{
		ID: r.ID, Status: r.Status, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
		Frames: int(r.Frames.Int32), Error: r.Error.String,
	}
}
