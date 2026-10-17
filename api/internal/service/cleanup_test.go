package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The test database is shared with other packages' tests, so these tests check their own
// rows, never the run's totals (which include everyone's).

func tenantExec(t *testing.T, pool *pgxpool.Pool, space uuid.UUID, sql string, args ...any) {
	t.Helper()
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "SELECT set_config('app.tenant_id', $1, true)", space.String()); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func tenantCount(t *testing.T, pool *pgxpool.Pool, space uuid.UUID, sql string, args ...any) int {
	t.Helper()
	var n int
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), "SELECT set_config('app.tenant_id', $1, true)", space.String()); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func randomHash() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

func exists(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), "SELECT EXISTS ("+sql+")", args...).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func days(n int) string { return fmt.Sprintf("%d days", n) }

// oldPixels inserts n pixels placed age ago on the fixture's canvas.
func (f *snapshotFixture) oldPixels(t *testing.T, n int, age string) {
	t.Helper()
	tenantExec(t, f.pool, f.space, `INSERT INTO pixels (space_id, canvas_id, x, y, color, placed_at)
		SELECT $1, $2, (g % 256)::smallint, 0, 1, now() - $3::interval FROM generate_series(1, $4::int) g`,
		f.space, f.canvas, age, n)
}

func (f *snapshotFixture) pixelCount(t *testing.T, olderThan string) int {
	return tenantCount(t, f.pool, f.space,
		"SELECT count(*) FROM pixels WHERE canvas_id = $1 AND placed_at < now() - $2::interval", f.canvas, olderThan)
}

func TestCleanupRules(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	user := newUser(t, f.pool, "x")
	svc := NewCleanup(f.pool, f.objects)

	session := func(expires, revoked string) []byte { // revoked "": not revoked
		h := randomHash()
		if _, err := f.pool.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at, revoked_at)
			VALUES ($1, $2, now() + $3::interval, now() + NULLIF($4, '')::interval)`, user, h, expires, revoked); err != nil {
			t.Fatal(err)
		}
		return h
	}
	expired8, expired6 := session("-8 days", ""), session("-6 days", "")
	revoked8 := session("30 days", "-8 days")
	live := session("30 days", "")

	ticket := func(expires string) []byte {
		h := randomHash()
		if _, err := f.pool.Exec(ctx, "INSERT INTO ws_tickets (ticket_hash, user_id, expires_at) VALUES ($1, $2, now() + $3::interval)",
			h, user, expires); err != nil {
			t.Fatal(err)
		}
		return h
	}
	ticketOld, ticketNew := ticket("-2 hours"), ticket("-30 minutes")

	invite := func(expires string) []byte {
		h := randomHash()
		if _, err := f.pool.Exec(ctx, `INSERT INTO invites (space_id, code_hash, created_by, expires_at)
			VALUES ($1, $2, $3, now() + $4::interval)`, f.space, h, user, expires); err != nil {
			t.Fatal(err)
		}
		return h
	}
	inviteOld, inviteNew := invite("-31 days"), invite("-29 days")

	// A pixel 31 days old that is still on the board: gone from the history, still on the board.
	f.oldPixels(t, 1, days(31))
	f.oldPixels(t, 1, days(29))
	f.paint(t, 7, 7, 3)

	template := func(status, age string) (uuid.UUID, string) {
		id := uuid.New()
		key := TemplateKey(f.space, id)
		f.objects.put(key, []byte("png"), "image/png")
		tenantExec(t, f.pool, f.space, `INSERT INTO templates (id, space_id, canvas_id, uploaded_by, object_key, x, y, bytes, status, created_at)
			VALUES ($1, $2, $3, $4, $5, 0, 0, 3, $6, now() - $7::interval)`, id, f.space, f.canvas, user, key, status, age)
		return id, key
	}
	staleID, staleKey := template("pending", "2 days")
	freshID, _ := template("pending", "1 hour")
	readyID, readyKey := template("ready", "2 days")

	if _, err := svc.Run(ctx, time.Now(), false); err != nil {
		t.Fatal(err)
	}

	hashGone := func(table, col string, h []byte) bool {
		return !exists(t, f.pool, fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1", table, col), h)
	}
	for name, c := range map[string]struct {
		gone bool
		want bool
	}{
		"session expired 8 days ago": {hashGone("sessions", "token_hash", expired8), true},
		"session revoked 8 days ago": {hashGone("sessions", "token_hash", revoked8), true},
		"session expired 6 days ago": {hashGone("sessions", "token_hash", expired6), false},
		"live session":               {hashGone("sessions", "token_hash", live), false},
		"ticket expired 2 hours ago": {hashGone("ws_tickets", "ticket_hash", ticketOld), true},
		"ticket expired 30 min ago":  {hashGone("ws_tickets", "ticket_hash", ticketNew), false},
		"invite expired 31 days ago": {hashGone("invites", "code_hash", inviteOld), true},
		"invite expired 29 days ago": {hashGone("invites", "code_hash", inviteNew), false},
		"pending template, 2 days":   {tenantCount(t, f.pool, f.space, "SELECT count(*) FROM templates WHERE id = $1", staleID) == 0, true},
		"pending template, 1 hour":   {tenantCount(t, f.pool, f.space, "SELECT count(*) FROM templates WHERE id = $1", freshID) == 0, false},
		"ready template, 2 days":     {tenantCount(t, f.pool, f.space, "SELECT count(*) FROM templates WHERE id = $1", readyID) == 0, false},
		"stale template's file":      {!f.objects.has(staleKey), true},
		"ready template's file":      {!f.objects.has(readyKey), false},
		"pixel placed 31 days ago":   {f.pixelCount(t, days(30)) == 0, true},
		"pixel placed 29 days ago":   {f.pixelCount(t, days(0)) == 0, false},
		"the board's painted cell":   {tenantCount(t, f.pool, f.space, "SELECT count(*) FROM canvas_pixels WHERE canvas_id = $1", f.canvas) == 0, false},
	} {
		if c.gone != c.want {
			t.Errorf("%s: deleted = %v, want %v", name, c.gone, c.want)
		}
	}

	// Twice in a row: nothing of ours left to delete.
	n, err := svc.Run(ctx, time.Now(), false)
	if err != nil || f.pixelCount(t, days(30)) != 0 {
		t.Fatalf("second run: %+v, %v", n, err)
	}
	if !f.objects.has(readyKey) {
		t.Error("second run deleted a ready template's file")
	}
}

func TestCleanupTimelapses(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	insert := func(status string, age time.Duration) uuid.UUID {
		id := uuid.New()
		var key any
		if status == "ready" {
			k := TimelapseKey(f.space, f.canvas, id)
			f.objects.put(k, []byte("gif"), "image/gif")
			key = k
		}
		tenantExec(t, f.pool, f.space, `INSERT INTO timelapses (id, space_id, canvas_id, status, object_key, created_at)
			VALUES ($1, $2, $3, $4, $5, now() - $6::interval)`, id, f.space, f.canvas, status, key, age.String())
		return id
	}
	pending := insert("pending", 0)
	var ready []uuid.UUID
	for i := range 12 {
		ready = append(ready, insert("ready", time.Duration(i+1)*time.Hour))
	}
	failedOld := insert("failed", 8*24*time.Hour)

	if _, err := NewCleanup(f.pool, f.objects).Run(ctx, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	left := func(id uuid.UUID) bool {
		return tenantCount(t, f.pool, f.space, "SELECT count(*) FROM timelapses WHERE id = $1", id) == 1
	}
	// Newest first: pending, then ready[0..8] are the 10 kept; ready[9..11] and the old failed one go.
	for i, id := range ready {
		key := TimelapseKey(f.space, f.canvas, id)
		if keep := i < 9; left(id) != keep || f.objects.has(key) != keep {
			t.Errorf("ready #%d: row %v, file %v, want both %v", i, left(id), f.objects.has(key), keep)
		}
	}
	if !left(pending) {
		t.Error("a pending timelapse was deleted")
	}
	if left(failedOld) {
		t.Error("a failed timelapse 8 days old was kept")
	}
}

// A file that cannot be deleted keeps its row, so tomorrow's run tries again.
func TestCleanupBucketFirst(t *testing.T) {
	f := newSnapshotFixture(t)
	ctx := context.Background()
	user := newUser(t, f.pool, "x")
	id := uuid.New()
	key := TemplateKey(f.space, id)
	f.objects.put(key, []byte("png"), "image/png")
	tenantExec(t, f.pool, f.space, `INSERT INTO templates (id, space_id, canvas_id, uploaded_by, object_key, x, y, bytes, created_at)
		VALUES ($1, $2, $3, $4, $5, 0, 0, 3, now() - interval '2 days')`, id, f.space, f.canvas, user, key)
	f.objects.delErr = errors.New("bucket down")

	if _, err := NewCleanup(f.pool, f.objects).Run(ctx, time.Now(), false); err == nil {
		t.Error("a failed file delete was not reported")
	}
	if tenantCount(t, f.pool, f.space, "SELECT count(*) FROM templates WHERE id = $1", id) != 1 {
		t.Error("the row went although its file is still there")
	}
}

func TestCleanupPixelsBatchesSpacesAndDryRun(t *testing.T) {
	a, b := newSnapshotFixture(t), newSnapshotFixture(t)
	ctx := context.Background()
	a.oldPixels(t, 12000, days(31)) // three batches
	b.oldPixels(t, 3, days(31))
	a.oldPixels(t, 5, days(1))
	b.oldPixels(t, 5, days(1))
	svc := NewCleanup(a.pool, a.objects)

	dry, err := svc.Run(ctx, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Pixels < 12003 || a.pixelCount(t, days(30)) != 12000 || b.pixelCount(t, days(30)) != 3 {
		t.Fatalf("dry run: counted %d, and must delete nothing", dry.Pixels)
	}

	got, err := svc.Run(ctx, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pixels < 12003 {
		t.Errorf("deleted %d pixels, want at least 12003", got.Pixels)
	}
	for name, f := range map[string]*snapshotFixture{"a": a, "b": b} {
		if old, recent := f.pixelCount(t, days(30)), f.pixelCount(t, "0 days"); old != 0 || recent != 5 {
			t.Errorf("space %s: %d old and %d recent left, want 0 and 5", name, old, recent)
		}
	}
}
