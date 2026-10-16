package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeQueue records what would go to the worker; err makes queueing fail.
type fakeQueue struct {
	mu         sync.Mutex
	thumbnails []uuid.UUID
	timelapses []uuid.UUID
	err        error
}

func (q *fakeQueue) Thumbnail(_ context.Context, _, templateID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.thumbnails = append(q.thumbnails, templateID)
	return nil
}

func (q *fakeQueue) Snapshots(context.Context, time.Time) error { return q.err }

func (q *fakeQueue) Timelapse(_ context.Context, _, timelapseID uuid.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.timelapses = append(q.timelapses, timelapseID)
	return nil
}

// stripes is a w × h PNG of vertical red and blue stripes, one pixel wide.
func stripes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: 255, A: 255}
			if x%2 == 1 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnail(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	queue := &fakeQueue{}
	f.svc.WithQueue(queue)

	id, _, err := f.upload(t, stripes(t, 200, 100))
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.thumbnails) != 1 || queue.thumbnails[0] != id {
		t.Fatalf("queued %v, want the new template", queue.thumbnails)
	}
	for range 2 { // the job is safe to run twice
		if err := f.svc.MakeThumbnail(ctx, f.space, id); err != nil {
			t.Fatal(err)
		}
	}
	data, err := f.objects.Get(ctx, TemplateThumbKey(f.space, id), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := thumb.Bounds(); b.Dx() != 64 || b.Dy() != 32 {
		t.Errorf("thumbnail %v, want 64 × 32", b)
	}
	for x := range 64 { // nearest neighbour: only the original's two colours, never a blend
		r, g, b, _ := thumb.At(x, 5).RGBA()
		if g != 0 || (r != 0xffff) == (b != 0xffff) {
			t.Fatalf("pixel %d is a blend: %d %d %d", x, r, g, b)
		}
	}
	list, _ := f.svc.List(ctx, f.space, f.canvas)
	if len(list) != 1 || list[0].ThumbURL == "" {
		t.Errorf("list: %+v, want a thumbnail URL", list)
	}

	if err := f.svc.Delete(ctx, f.space, f.canvas, id, f.member); err != nil {
		t.Fatal(err)
	}
	if f.objects.has(TemplateThumbKey(f.space, id)) {
		t.Error("thumbnail left behind after the template was deleted")
	}
}

func TestThumbnailSmallAndUnqueued(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	f.svc.WithQueue(&fakeQueue{err: errors.New("queue down")})

	id, tpl, err := f.upload(t, stripes(t, 40, 30))
	if err != nil || tpl.URL == "" {
		t.Fatalf("a queue failure must not fail the upload: %v", err)
	}
	if err := f.svc.MakeThumbnail(ctx, f.space, id); err != nil {
		t.Fatal(err)
	}
	data, _ := f.objects.Get(ctx, TemplateThumbKey(f.space, id), 1<<20)
	if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 40 || cfg.Height != 30 {
		t.Errorf("small template: %+v, %v; want 40 × 30, not blown up", cfg, err)
	}

	// The stored file changed after it was checked: refused before decoding.
	f.objects.put(TemplateKey(f.space, id), bombPNG(), "image/png")
	if err := f.svc.MakeThumbnail(ctx, f.space, id); err == nil {
		t.Error("a file that no longer passes the checks was thumbnailed")
	}
}

// timelapseFixture: a canvas with n hourly snapshots, each one more pixel.
func timelapseFixture(t *testing.T, n int) (*snapshotFixture, *Timelapses, *fakeQueue, uuid.UUID) {
	f := newSnapshotFixture(t)
	for i := range n {
		f.paint(t, i, 0, 5)
		f.svc.Pass(context.Background(), hour1.Add(time.Duration(i)*time.Hour))
	}
	queue := &fakeQueue{}
	user := newUser(t, f.pool, "x")
	return f, NewTimelapses(f.pool, f.objects, queue), queue, user
}

func TestTimelapseBuild(t *testing.T) {
	f, svc, queue, user := timelapseFixture(t, 3)
	ctx := context.Background()

	tl, err := svc.Request(ctx, f.space, f.canvas, user)
	if err != nil || tl.Status != "pending" || len(queue.timelapses) != 1 {
		t.Fatalf("request: %+v, %v, queued %d", tl, err, len(queue.timelapses))
	}
	if _, err := svc.Request(ctx, f.space, f.canvas, user); !errors.Is(err, ErrTimelapsePending) {
		t.Errorf("second request while pending: err = %v, want ErrTimelapsePending", err)
	}

	for range 2 { // safe to run twice
		if err := svc.Build(ctx, f.space, tl.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.List(ctx, f.space, f.canvas)
	if err != nil || len(list) != 1 || list[0].Status != "ready" || list[0].Frames != 3 || list[0].URL == "" {
		t.Fatalf("list: %+v, %v", list, err)
	}
	data, _ := f.objects.Get(ctx, TimelapseKey(f.space, f.canvas, tl.ID), 1<<24)
	anim, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(anim.Image) != 3 {
		t.Fatalf("gif: %v, %d frames; want 3", err, len(anim.Image))
	}
	// Oldest first: frame 1 has one pixel, frame 3 has three.
	if anim.Image[0].ColorIndexAt(1, 0) != 0 || anim.Image[2].ColorIndexAt(2, 0) != 5 {
		t.Error("frames are not the snapshots in time order")
	}
}

func TestTimelapseNotEnoughSnapshots(t *testing.T) {
	f, svc, _, user := timelapseFixture(t, 1)
	ctx := context.Background()
	tl, err := svc.Request(ctx, f.space, f.canvas, user)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Build(ctx, f.space, tl.ID, false); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List(ctx, f.space, f.canvas)
	if list[0].Status != "failed" || list[0].Error == "" {
		t.Errorf("%+v, want failed with a reason", list[0])
	}
}

// A timelapse never stays pending for good: not after its last retry, not after a lost job.
func TestTimelapseNeverStuckPending(t *testing.T) {
	f, svc, _, user := timelapseFixture(t, 2)
	ctx := context.Background()
	tl, _ := svc.Request(ctx, f.space, f.canvas, user)
	f.objects.Delete(ctx, SnapshotKey(f.space, f.canvas, hour1)) // the bucket loses a frame

	if err := svc.Build(ctx, f.space, tl.ID, false); err == nil {
		t.Fatal("build with a missing frame succeeded")
	}
	if list, _ := svc.List(ctx, f.space, f.canvas); list[0].Status != "pending" {
		t.Fatalf("after a failure with retries left: %s, want still pending", list[0].Status)
	}
	if err := svc.Build(ctx, f.space, tl.ID, true); err == nil {
		t.Fatal("last attempt hid its error")
	}
	if list, _ := svc.List(ctx, f.space, f.canvas); list[0].Status != "failed" {
		t.Errorf("after the last attempt: %s, want failed", list[0].Status)
	}

	// A job lost with Redis's data: an old pending one does not block the canvas.
	stuck, err := svc.Request(ctx, f.space, f.canvas, user)
	if err != nil {
		t.Fatal(err)
	}
	err = pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", f.space.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE timelapses SET created_at = now() - interval '11 minutes' WHERE id = $1", stuck.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Request(ctx, f.space, f.canvas, user); err != nil {
		t.Errorf("request after a stale pending one: %v, want it accepted", err)
	}
}

func TestTimelapseQueueDown(t *testing.T) {
	f, svc, queue, user := timelapseFixture(t, 2)
	ctx := context.Background()
	queue.err = errors.New("redis down")
	if _, err := svc.Request(ctx, f.space, f.canvas, user); !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("err = %v, want ErrQueueUnavailable", err)
	}
	if list, _ := svc.List(ctx, f.space, f.canvas); len(list) != 0 {
		t.Errorf("%d rows left behind, want none", len(list))
	}
	if _, err := NewTimelapses(f.pool, f.objects, nil).Request(ctx, f.space, f.canvas, user); !errors.Is(err, ErrQueueUnavailable) {
		t.Errorf("no queue at all: err = %v, want ErrQueueUnavailable", err)
	}
}

func TestTimelapsesRLS(t *testing.T) {
	f, svc, _, user := timelapseFixture(t, 2)
	if _, err := svc.Request(context.Background(), f.space, f.canvas, user); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM timelapses").Scan(&n); err != nil || n != 0 {
		t.Errorf("no tenant: %d rows visible, err %v; want 0", n, err)
	}
}
