package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// testPool connects to TEST_DATABASE_URL as the app role, so RLS applies.
// Skips when unset: run with make test-db.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE app")
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// Guard: a superuser would skip RLS and make the isolation tests meaningless.
	var super bool
	if err := pool.QueryRow(context.Background(), "SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&super); err != nil || super {
		t.Fatalf("tests must run as a non-superuser (super=%v, err=%v)", super, err)
	}
	return pool
}

func newSpace(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	slug := "test-" + uuid.NewString()[:8]
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO spaces (name, slug) VALUES ($1, $1) RETURNING id", slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM spaces WHERE id = $1", id)
	})
	return id
}

// newUser inserts a user with the given password (cheap bcrypt cost; Login accepts any cost).
func newUser(t *testing.T, pool *pgxpool.Pool, password string) uuid.UUID {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	var id uuid.UUID
	email := "test-" + uuid.NewString()[:8] + "@example.com"
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, 'Tester') RETURNING id",
		email, string(hash)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id)
	})
	return id
}

func emailOf(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := pool.QueryRow(context.Background(), "SELECT email FROM users WHERE id = $1", id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

func addMember(t *testing.T, pool *pgxpool.Pool, space, user uuid.UUID, role string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO space_members (space_id, user_id, role) VALUES ($1, $2, $3)", space, user, role); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	space := newSpace(t, pool)
	user := newUser(t, pool, "pw-not-used")
	svc := NewCanvases(pool)

	c, err := svc.Create(ctx, space, user, "first", DefaultCooldown)
	if err != nil {
		t.Fatal(err)
	}
	if c.Width != 256 || c.Palette.ID != "classic16" || len(c.Palette.Colors) != 16 {
		t.Errorf("defaults: got %+v", c)
	}

	list, err := svc.List(ctx, space)
	if err != nil || len(list) != 1 || list[0].ID != c.ID {
		t.Fatalf("list: got %v, %v", list, err)
	}

	// Paint one cell directly; placing arrives in Step 6.
	err = inTenantExec(ctx, pool, space,
		"INSERT INTO canvas_pixels (canvas_id, x, y, space_id, color, pixel_id) VALUES ($1, 3, 2, $2, 5, 1)", c.ID, space)
	if err != nil {
		t.Fatal(err)
	}

	board, err := svc.Board(ctx, space, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(board) != 256*256 {
		t.Fatalf("board length = %d, want 65536", len(board))
	}
	if board[2*256+3] != 5 || board[0] != 0 {
		t.Errorf("board cells: (3,2)=%d want 5, (0,0)=%d want 0", board[2*256+3], board[0])
	}
}

func TestOtherSpaceSeesNothing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	a, b := newSpace(t, pool), newSpace(t, pool)
	svc := NewCanvases(pool)

	c, err := svc.Create(ctx, a, newUser(t, pool, "x"), "a's canvas", DefaultCooldown)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Board(ctx, b, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("board from space b: err = %v, want ErrNotFound", err)
	}
	if list, err := svc.List(ctx, b); err != nil || len(list) != 0 {
		t.Errorf("list from space b: got %d canvases, err %v", len(list), err)
	}
}

// RLS on its own, without the Go-side space_id filter.
func TestRLS(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	a, b := newSpace(t, pool), newSpace(t, pool)
	if _, err := NewCanvases(pool).Create(ctx, a, newUser(t, pool, "x"), "rls", DefaultCooldown); err != nil {
		t.Fatal(err)
	}

	count := func(tenant uuid.UUID) int {
		var n int
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if tenant != uuid.Nil {
				if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctx, "SELECT count(*) FROM canvases WHERE space_id IN ($1, $2)", a, b).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := count(uuid.Nil); n != 0 {
		t.Errorf("no tenant set: saw %d rows, want 0", n)
	}
	if n := count(b); n != 0 {
		t.Errorf("tenant b: saw %d of a's rows, want 0", n)
	}
	if n := count(a); n != 1 {
		t.Errorf("tenant a: saw %d rows, want 1", n)
	}

	// WITH CHECK: tenant a cannot write a row into space b.
	err := inTenantExec(ctx, pool, a, "INSERT INTO canvases (space_id, name) VALUES ($1, 'sneaky')", b)
	if err == nil {
		t.Error("insert into another space succeeded, want an RLS violation")
	}
}

func inTenantExec(ctx context.Context, pool *pgxpool.Pool, tenant uuid.UUID, sql string, args ...any) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, sql, args...)
		return err
	})
}
