package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
)

// testRedis connects to TEST_REDIS_URL. Keys carry random space ids, so tests share it safely.
func testRedis(t *testing.T) *Redis {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	c, err := platform.NewRedis(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return NewRedis(c)
}

// downRedis points at a closed port: every call fails, as when Redis is stopped.
func downRedis(t *testing.T) *Redis {
	t.Helper()
	c, err := platform.NewRedis("redis://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return NewRedis(c)
}

func TestBoardCacheCompareAndSet(t *testing.T) {
	f := newSnapshotFixture(t)
	r := testRedis(t)
	ctx := context.Background()
	c := NewCanvases(f.pool).WithRedis(r)
	if _, err := c.Board(ctx, f.space, f.canvas); err != nil { // builds the cached board
		t.Fatal(err)
	}

	r.cacheCell(ctx, f.space, f.canvas, 256, 1, 1, 5, 7)
	r.cacheCell(ctx, f.space, f.canvas, 256, 1, 1, 9, 5) // older, arrives later
	r.cacheCell(ctx, f.space, f.canvas, 256, 2, 2, 9, 5)
	r.cacheCell(ctx, f.space, f.canvas, 256, 2, 2, 5, 7) // newer, arrives later
	board, err := c.Board(ctx, f.space, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	if board[1*256+1] != 5 || board[2*256+2] != 5 {
		t.Errorf("cells = %d, %d; want the newer pixel's color 5 in both", board[1*256+1], board[2*256+2])
	}
}

// A pixel announced while the board was being rebuilt must survive the rebuild.
func TestBoardRebuildMergesNewerPixels(t *testing.T) {
	f := newSnapshotFixture(t)
	r := testRedis(t)
	ctx := context.Background()
	f.paint(t, 2, 2, 6)                                    // pixel 1 in Postgres
	f.paint(t, 4, 4, 7)                                    // pixel 2
	r.cacheCell(ctx, f.space, f.canvas, 256, 2, 2, 3, 100) // newer than Postgres's read, no board yet
	r.cacheCell(ctx, f.space, f.canvas, 256, 9, 9, 4, 50)  // a cell Postgres has not returned at all

	board, err := NewCanvases(f.pool).WithRedis(r).Board(ctx, f.space, f.canvas)
	if err != nil {
		t.Fatal(err)
	}
	if board[2*256+2] != 3 || board[4*256+4] != 7 || board[9*256+9] != 4 {
		t.Errorf("(2,2)=%d (4,4)=%d (9,9)=%d; want 3 (newer in Redis), 7 (Postgres), 4 (Redis only)",
			board[2*256+2], board[4*256+4], board[9*256+9])
	}

	// Postgres's id is now in ver: a late write of an older pixel does not win.
	r.cacheCell(ctx, f.space, f.canvas, 256, 4, 4, 1, 1)
	board, _ = NewCanvases(f.pool).WithRedis(r).Board(ctx, f.space, f.canvas)
	if board[4*256+4] != 7 {
		t.Errorf("(4,4) = %d after an older write, want 7", board[4*256+4])
	}
}

func TestBoardSingleFlight(t *testing.T) {
	f := newSnapshotFixture(t)
	r := testRedis(t)
	f.paint(t, 3, 3, 5)
	c := NewCanvases(f.pool).WithRedis(r)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if b, err := c.Board(context.Background(), f.space, f.canvas); err != nil || b[3*256+3] != 5 {
				t.Errorf("board: err %v", err)
			}
		})
	}
	wg.Wait()
	if n := c.scans.Load(); n != 1 {
		t.Errorf("20 cold reads ran %d Postgres scans, want 1", n)
	}
}

func TestBoardCacheMatchesPostgres(t *testing.T) {
	r := testRedis(t)
	space, _, canvas, svc, _ := memberWithCanvas(t, 10)
	ctx := context.Background()
	svc.WithRedis(r)
	cached := NewCanvases(svc.pool).WithRedis(r)
	if _, err := cached.Board(ctx, space, canvas); err != nil {
		t.Fatal(err)
	}

	for i := range 6 { // one placement per member: the cooldown is per member
		u := newUser(t, svc.pool, "x")
		addMember(t, svc.pool, space, u, "member")
		if _, err := svc.Place(ctx, space, canvas, u, i%3, 0, i+1, "web"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cached.Board(ctx, space, canvas)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := NewCanvases(svc.pool).Board(ctx, space, canvas)
	if !bytes.Equal(got, want) {
		t.Error("the cached board differs from Postgres's")
	}
	if cached.scans.Load() != 1 {
		t.Errorf("%d rebuilds, want 1: placements must update the cache, not drop it", cached.scans.Load())
	}
}

func TestCooldownInRedis(t *testing.T) {
	r := testRedis(t)
	space, user, canvas, svc, _ := memberWithCanvas(t, 60)
	ctx := context.Background()
	svc.WithRedis(r)

	var wg sync.WaitGroup
	var mu sync.Mutex
	placed, refused := 0, 0
	for range 5 {
		wg.Go(func() {
			_, err := svc.Place(ctx, space, canvas, user, 1, 1, 1, "web")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				placed++
			case errors.As(err, new(*CooldownError)):
				refused++
			default:
				t.Errorf("unexpected: %v", err)
			}
		})
	}
	wg.Wait()
	if placed != 1 || refused != 4 {
		t.Errorf("placed %d, refused %d; want 1 and 4", placed, refused)
	}

	// Redis forgot (a restart): Postgres refuses, and Redis takes Postgres's time.
	r.c.Del(ctx, cooldownKey(space, user))
	if _, err := svc.Place(ctx, space, canvas, user, 1, 1, 1, "web"); !errors.As(err, new(*CooldownError)) {
		t.Fatalf("after Redis forgot: err = %v, want CooldownError from Postgres", err)
	}
	left, ok := r.claimCooldown(ctx, space, user, 60)
	if !ok || left < 50*time.Second {
		t.Errorf("Redis now says %v left, want about a minute", left)
	}

	// A placement that fails after the Redis claim gives the turn back.
	stranger := newUser(t, svc.pool, "x")
	if _, err := svc.Place(ctx, space, canvas, stranger, 1, 1, 1, "web"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not a member: err = %v", err)
	}
	if n, _ := r.c.Exists(ctx, cooldownKey(space, stranger)).Result(); n != 0 {
		t.Error("a failed placement kept its Redis cooldown")
	}
}

// The turn is per space, the cooldown per canvas: a short canvas opens before a long one.
func TestCooldownPerCanvas(t *testing.T) {
	r := testRedis(t)
	space, user, long, svc, _ := memberWithCanvas(t, 60)
	ctx := context.Background()
	svc.WithRedis(r)
	short, err := NewCanvases(svc.pool).Create(ctx, space, user, "short", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Place(ctx, space, long, user, 0, 0, 1, "web"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if _, err := svc.Place(ctx, space, short.ID, user, 0, 0, 1, "web"); err != nil {
		t.Errorf("2 s canvas after 2.1 s: %v, want placed", err)
	}
}

func TestTicketsInRedis(t *testing.T) {
	r := testRedis(t)
	pool := testPool(t)
	ctx := context.Background()
	user := newUser(t, pool, "x")
	auth := NewAuth(pool).WithRedis(r)

	ticket, _, err := auth.CreateWSTicket(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := auth.RedeemTicket(ctx, ticket); err != nil || got != user {
		t.Fatalf("redeem: %v, %v", got, err)
	}
	if _, err := auth.RedeemTicket(ctx, ticket); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("second use: err = %v, want ErrUnauthenticated", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ws_tickets WHERE user_id = $1", user).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d tickets in Postgres, want 0", n)
	}

	ticket, _, _ = auth.CreateWSTicket(ctx, user)
	r.c.PExpire(ctx, ticketKey(hashSecret(ticket)), 50*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	if _, err := auth.RedeemTicket(ctx, ticket); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expired: err = %v, want ErrUnauthenticated", err)
	}

	ticket, _, _ = auth.CreateWSTicket(ctx, user)
	if _, err := pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.RedeemTicket(ctx, ticket); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("suspended user: err = %v, want ErrUnauthenticated", err)
	}
}

// With Redis stopped, everything still works from Postgres; nothing returns an error,
// and after the first failure nothing waits for Redis either.
func TestRedisDown(t *testing.T) {
	r := downRedis(t)
	space, user, canvas, svc, _ := memberWithCanvas(t, 10)
	ctx := context.Background()
	svc.WithRedis(r)
	start := time.Now()
	defer func() {
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("took %v with Redis down, want well under 2 s: the breaker should skip Redis", d)
		}
	}()

	if _, err := svc.Place(ctx, space, canvas, user, 5, 5, 3, "web"); err != nil {
		t.Fatalf("place: %v", err)
	}
	if _, err := svc.Place(ctx, space, canvas, user, 5, 5, 3, "web"); !errors.As(err, new(*CooldownError)) {
		t.Errorf("cooldown without Redis: err = %v, want CooldownError from Postgres", err)
	}
	board, err := NewCanvases(svc.pool).WithRedis(r).Board(ctx, space, canvas)
	if err != nil || board[5*256+5] != 3 {
		t.Errorf("board: err %v, want it from Postgres", err)
	}
	auth := NewAuth(svc.pool).WithRedis(r)
	ticket, _, err := auth.CreateWSTicket(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := auth.RedeemTicket(ctx, ticket); err != nil || got != user {
		t.Errorf("ticket through Postgres: %v, %v", got, err)
	}
	if r.errors.Load() == 0 {
		t.Error("no fallback counted")
	}
}

// A placement made while Redis was skipped must not leave a stale cached board behind.
func TestBoardStaleAfterOutage(t *testing.T) {
	r := testRedis(t)
	space, user, canvas, svc, _ := memberWithCanvas(t, 10)
	ctx := context.Background()
	svc.WithRedis(r)
	cached := NewCanvases(svc.pool).WithRedis(r)
	if _, err := cached.Board(ctx, space, canvas); err != nil {
		t.Fatal(err)
	}

	r.Failed("test", errors.New("pretend Redis went away")) // opens the breaker
	if _, err := svc.Place(ctx, space, canvas, user, 7, 7, 4, "web"); err != nil {
		t.Fatal(err)
	}
	r.downUntil.Store(0) // Redis is back

	board, err := cached.Board(ctx, space, canvas)
	if err != nil || board[7*256+7] != 4 {
		t.Errorf("(7,7) = %d, err %v; want 4: the stale board must be rebuilt", board[7*256+7], err)
	}
}
