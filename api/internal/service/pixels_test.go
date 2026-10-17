package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/realtime"
)

type recorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) Publish(_, canvasID uuid.UUID, _ any) {
	r.mu.Lock()
	r.msgs = append(r.msgs, realtime.CanvasTopic(canvasID))
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// member sets up a space, a member and a canvas with the given cooldown.
func memberWithCanvas(t *testing.T, cooldown int) (space, user, canvas uuid.UUID, svc *Pixels, rec *recorder) {
	pool := testPool(t)
	space, user = newSpace(t, pool), newUser(t, pool, "x")
	addMember(t, pool, space, user, "member")
	c, err := NewCanvases(pool).Create(context.Background(), space, user, "pixels", cooldown)
	if err != nil {
		t.Fatal(err)
	}
	rec = &recorder{}
	return space, user, c.ID, NewPixels(pool, rec), rec
}

func TestPlaceShowsOnBoardAndHistory(t *testing.T) {
	space, user, canvas, svc, rec := memberWithCanvas(t, 10)
	ctx := context.Background()

	p, err := svc.Place(ctx, space, canvas, user, 7, 9, 5, "web")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.NextPlaceAt.Sub(p.PlacedAt); got < 9*time.Second || got > 11*time.Second {
		t.Errorf("next placement in %v, want about 10s", got)
	}
	board, err := NewCanvases(svc.pool).Board(ctx, space, canvas)
	if err != nil || board[9*256+7] != 5 {
		t.Errorf("board (7,9) = %d, err %v; want 5", board[9*256+7], err)
	}
	var n int
	_ = pgx.BeginFunc(ctx, svc.pool, func(tx pgx.Tx) error {
		_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", space.String())
		return tx.QueryRow(ctx, "SELECT count(*) FROM pixels WHERE canvas_id = $1 AND id = $2", canvas, p.PixelID).Scan(&n)
	})
	if n != 1 {
		t.Errorf("history rows = %d, want 1", n)
	}
	if rec.count() != 1 {
		t.Errorf("published %d messages, want 1", rec.count())
	}
}

func TestCooldown(t *testing.T) {
	space, user, canvas, svc, rec := memberWithCanvas(t, 60)
	ctx := context.Background()

	first, err := svc.Place(ctx, space, canvas, user, 0, 0, 1, "web")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Place(ctx, space, canvas, user, 1, 1, 2, "web")
	var cd *CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("second placement: err = %v, want CooldownError", err)
	}
	if d := cd.NextPlaceAt.Sub(first.PlacedAt); d < 59*time.Second || d > 61*time.Second {
		t.Errorf("NextPlaceAt is %v after the first placement, want about 60s", d)
	}
	if rec.count() != 1 {
		t.Errorf("published %d messages, want only the first", rec.count())
	}
}

func TestDoubleClickPlacesOnce(t *testing.T) {
	space, user, canvas, svc, rec := memberWithCanvas(t, 60)
	ctx := context.Background()

	const racers = 5
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.Place(ctx, space, canvas, user, i, i, 3, "web")
		}()
	}
	wg.Wait()

	placed := 0
	for _, err := range errs {
		var cd *CooldownError
		switch {
		case err == nil:
			placed++
		case !errors.As(err, &cd):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if placed != 1 || rec.count() != 1 {
		t.Errorf("%d placements and %d messages from %d simultaneous clicks, want 1 and 1", placed, rec.count(), racers)
	}
}

func TestPlaceRejectsBadInput(t *testing.T) {
	space, user, canvas, svc, _ := memberWithCanvas(t, 1)
	ctx := context.Background()

	for _, c := range [][3]int{{-1, 0, 0}, {256, 0, 0}, {0, 256, 0}, {0, 0, 16}, {0, 0, -1}} {
		if _, err := svc.Place(ctx, space, canvas, user, c[0], c[1], c[2], "web"); !errors.Is(err, ErrInvalidPixel) {
			t.Errorf("x=%d y=%d color=%d: err = %v, want ErrInvalidPixel", c[0], c[1], c[2], err)
		}
	}
	other := newSpace(t, svc.pool)
	if _, err := svc.Place(ctx, other, canvas, user, 0, 0, 0, "web"); !errors.Is(err, ErrNotFound) {
		t.Errorf("canvas through another space: err = %v, want ErrNotFound", err)
	}
}

func TestPixelsRLS(t *testing.T) {
	space, user, canvas, svc, _ := memberWithCanvas(t, 1)
	ctx := context.Background()
	if _, err := svc.Place(ctx, space, canvas, user, 0, 0, 1, "web"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := svc.pool.QueryRow(ctx, "SELECT count(*) FROM pixels WHERE canvas_id = $1", canvas).Scan(&n); err != nil || n != 0 {
		t.Errorf("no tenant set: saw %d pixels (err %v), want 0", n, err)
	}
}

func TestWSTickets(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := newUser(t, pool, "x")
	auth := NewAuth(pool)

	ticket, _, err := auth.CreateWSTicket(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := auth.RedeemTicket(ctx, ticket); err != nil || got != user {
		t.Fatalf("first use: %v, %v", got, err)
	}
	if _, err := auth.RedeemTicket(ctx, ticket); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("second use: err = %v, want ErrUnauthenticated", err)
	}
	if _, err := auth.RedeemTicket(ctx, "made-up"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("made-up: err = %v, want ErrUnauthenticated", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO ws_tickets (ticket_hash, user_id, expires_at) VALUES ($1, $2, now() - interval '1 second')",
		hashSecret("expired"), user); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.RedeemTicket(ctx, "expired"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expired: err = %v, want ErrUnauthenticated", err)
	}
}

func TestPlaceAs(t *testing.T) {
	space, user, canvas, svc, rec := memberWithCanvas(t, 10)
	ctx := context.Background()

	p, err := svc.PlaceAs(ctx, space, canvas, user, 3, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	var source string
	_ = pgx.BeginFunc(ctx, svc.pool, func(tx pgx.Tx) error {
		_, _ = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", space.String())
		return tx.QueryRow(ctx, "SELECT source FROM pixels WHERE id = $1", p.PixelID).Scan(&source)
	})
	if source != "bot" || rec.count() != 1 {
		t.Errorf("source %q, %d announced; want bot, 1", source, rec.count())
	}

	if _, err := svc.PlaceAs(ctx, space, canvas, user, 3, 4, 2); !errors.As(err, new(*CooldownError)) {
		t.Errorf("again at once: err = %v, want CooldownError", err)
	}

	stranger := newUser(t, svc.pool, "x")
	if _, err := svc.PlaceAs(ctx, space, canvas, stranger, 0, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("not a member: err = %v, want ErrNotFound", err)
	}

	suspended := newUser(t, svc.pool, "x")
	addMember(t, svc.pool, space, suspended, "member")
	if _, err := svc.pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", suspended); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlaceAs(ctx, space, canvas, suspended, 0, 0, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("suspended member: err = %v, want ErrNotFound", err)
	}
}
