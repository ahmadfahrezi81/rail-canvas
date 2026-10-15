package service

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

type snapshotFixture struct {
	pool          *pgxpool.Pool
	svc           *Snapshots
	objects       *fakeObjects
	space, canvas uuid.UUID
	nextPixel     int64
}

func newSnapshotFixture(t *testing.T) *snapshotFixture {
	pool := testPool(t)
	f := &snapshotFixture{pool: pool, objects: newFakeObjects(), space: newSpace(t, pool), nextPixel: 1}
	f.svc = NewSnapshots(pool, f.objects)
	user := newUser(t, pool, "x")
	c, err := NewCanvases(pool).Create(context.Background(), f.space, user, "snapshots", DefaultCooldown)
	if err != nil {
		t.Fatal(err)
	}
	f.canvas = c.ID
	return f
}

// paint sets one cell on the board directly, with a growing pixel id.
func (f *snapshotFixture) paint(t *testing.T, x, y, color int) {
	t.Helper()
	err := inTenant(context.Background(), f.pool, f.space, func(q *store.Queries) error {
		return q.UpsertCanvasPixel(context.Background(), store.UpsertCanvasPixelParams{
			CanvasID: f.canvas, X: int16(x), Y: int16(y), SpaceID: f.space, Color: int16(color), PixelID: f.nextPixel,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	f.nextPixel++
}

func (f *snapshotFixture) rows(t *testing.T) []store.ListSnapshotsRow {
	t.Helper()
	var rows []store.ListSnapshotsRow
	err := inTenant(context.Background(), f.pool, f.space, func(q *store.Queries) error {
		var err error
		rows, err = q.ListSnapshots(context.Background(), store.ListSnapshotsParams{CanvasID: f.canvas, SpaceID: f.space, MaxRows: 100})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

var hour1 = time.Date(2030, 1, 1, 10, 0, 0, 0, time.UTC)

func TestRenderPNG(t *testing.T) {
	p, _ := paletteByID("classic16")
	data, err := renderPNG(256, 256, p, []store.ListCanvasCellsRow{{X: 0, Y: 0, Color: 5}, {X: 255, Y: 255, Color: 13}})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 256 {
		t.Fatalf("size %v, want 256 × 256", b)
	}
	check := func(x, y int, wantR, wantG, wantB uint32) {
		r, g, b, _ := img.At(x, y).RGBA()
		if r>>8 != wantR || g>>8 != wantG || b>>8 != wantB {
			t.Errorf("(%d, %d) = #%02X%02X%02X, want #%02X%02X%02X", x, y, r>>8, g>>8, b>>8, wantR, wantG, wantB)
		}
	}
	check(0, 0, 0xE5, 0x00, 0x00)     // 5, red
	check(255, 255, 0x00, 0x00, 0xEA) // 13, blue
	check(10, 10, 0xFF, 0xFF, 0xFF)   // unset: the background
}

func TestSnapshotOnlyWhenChanged(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()

	f.svc.Pass(ctx, hour1)
	if n := len(f.rows(t)); n != 0 {
		t.Fatalf("empty canvas: %d snapshots, want 0", n)
	}

	f.paint(t, 1, 1, 5)
	f.svc.Pass(ctx, hour1.Add(5*time.Minute))
	key := SnapshotKey(f.space, f.canvas, hour1)
	if !strings.HasPrefix(key, "spaces/"+f.space.String()+"/snapshots/") || !f.objects.has(key) {
		t.Fatalf("no object at %s", key)
	}

	f.svc.Pass(ctx, hour1.Add(time.Hour))
	if n := len(f.rows(t)); n != 1 {
		t.Errorf("unchanged canvas: %d snapshots, want still 1", n)
	}

	f.paint(t, 2, 2, 6)
	f.svc.Pass(ctx, hour1.Add(2*time.Hour))
	rows := f.rows(t)
	if len(rows) != 2 || !rows[0].TakenAt.Equal(hour1.Add(2*time.Hour)) {
		t.Errorf("after a new pixel: %+v, want 2 with the newest first", rows)
	}
}

// Two API versions run side by side during a deploy; each takes a pass.
func TestSnapshotOncePerHour(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	f.paint(t, 1, 1, 5)
	f.svc.Pass(ctx, hour1)
	f.paint(t, 2, 2, 6)
	f.svc.Pass(ctx, hour1.Add(30*time.Minute))
	if n := len(f.rows(t)); n != 1 {
		t.Errorf("two passes in one hour: %d snapshots, want 1", n)
	}
}

func TestSnapshotFailedUploadLeavesNoRow(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	f.paint(t, 1, 1, 5)

	f.objects.putErr = errors.New("bucket down")
	if p := f.svc.Pass(ctx, hour1); p.Failed == 0 {
		t.Errorf("pass %+v, want a failure counted", p)
	}
	if n := len(f.rows(t)); n != 0 {
		t.Fatalf("failed upload: %d rows, want 0", n)
	}

	f.objects.putErr = nil
	f.svc.Pass(ctx, hour1.Add(10*time.Minute))
	if n := len(f.rows(t)); n != 1 {
		t.Errorf("retry in the same hour: %d rows, want 1", n)
	}
}

func TestSnapshotList(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	for i := range 3 {
		f.paint(t, i, 0, 5)
		f.svc.Pass(ctx, hour1.Add(time.Duration(i)*time.Hour))
	}
	list, err := f.svc.List(ctx, f.space, f.canvas)
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %d, %v; want 3", len(list), err)
	}
	if !list[0].TakenAt.Equal(hour1.Add(2*time.Hour)) || !strings.Contains(list[0].URL, "/snapshots/") {
		t.Errorf("newest first with a signed URL: %+v", list[0])
	}

	other := newSpace(t, f.pool)
	if _, err := f.svc.List(ctx, other, f.canvas); !errors.Is(err, ErrNotFound) {
		t.Errorf("another space's canvas: err = %v, want ErrNotFound", err)
	}
	if _, err := NewSnapshots(f.pool, nil).List(ctx, f.space, f.canvas); !errors.Is(err, ErrSnapshotsOff) {
		t.Errorf("no bucket: err = %v, want ErrSnapshotsOff", err)
	}
}

func TestSnapshotsRLS(t *testing.T) {
	f := newSnapshotFixture(t)
	f.paint(t, 1, 1, 5)
	f.svc.Pass(context.Background(), hour1)
	var n int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM snapshots").Scan(&n); err != nil || n != 0 {
		t.Errorf("no tenant: %d rows visible, err %v; want 0", n, err)
	}
}
