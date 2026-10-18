package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/db"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func TestSeedRefusesProduction(t *testing.T) {
	t.Setenv("RAILWAY_ENVIRONMENT_NAME", "production")
	t.Setenv("SEED_OWNER_PASSWORD", "long-enough-password")
	if err := seedCmd(false); err == nil {
		t.Error("cli seed in production: no error")
	}
	if err := seedCmd(true); err != nil {
		t.Errorf("predeploy in production: %v, want a silent skip", err)
	}
	t.Setenv("RAILWAY_ENVIRONMENT_NAME", "staging")
	t.Setenv("SEED_OWNER_PASSWORD", "")
	if err := seedCmd(true); err != nil {
		t.Errorf("predeploy without a password: %v, want a silent skip", err)
	}
	if err := seedCmd(false); err == nil {
		t.Error("cli seed without a password: no error")
	}
}

// A fresh database, as staging's first deploy sees it: migrate, app login, seed, each twice.
func TestFreshDatabase(t *testing.T) {
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	name := fmt.Sprintf("seed_test_%d", time.Now().UnixNano())
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx) }) // registered first, so it closes after the other cleanups
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)") })
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	fresh := u.String()

	// app is a cluster-wide role: put it back the way this server had it.
	var could bool
	if err := conn.QueryRow(ctx, "SELECT coalesce((SELECT rolcanlogin FROM pg_roles WHERE rolname = 'app'), false)").Scan(&could); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate(ctx, fresh, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "ALTER ROLE app NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !could {
			_, _ = conn.Exec(ctx, "ALTER ROLE app NOLOGIN PASSWORD NULL")
		}
	})

	if changed, err := appLogin(ctx, fresh, "app-pw-for-this-test"); err != nil || !changed {
		t.Fatalf("first app login: changed %v, err %v", changed, err)
	}
	if changed, err := appLogin(ctx, fresh, "another-pw"); err != nil || changed {
		t.Fatalf("second app login: changed %v, err %v; want unchanged", changed, err)
	}
	asApp := *u
	asApp.User = url.UserPassword("app", "app-pw-for-this-test")
	pool, err := pgxpool.New(ctx, asApp.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("app cannot log in with the password: %v", err)
	}

	if created, err := service.Seed(ctx, pool, "staging-owner-pw"); err != nil || !created {
		t.Fatalf("first seed: created %v, err %v", created, err)
	}
	if created, err := service.Seed(ctx, pool, "staging-owner-pw"); err != nil || created {
		t.Fatalf("second seed: created %v, err %v; want nothing", created, err)
	}
	var spaces, canvases int
	other, err := pgx.Connect(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close(ctx)
	if err := other.QueryRow(ctx, "SELECT (SELECT count(*) FROM spaces), (SELECT count(*) FROM canvases)").Scan(&spaces, &canvases); err != nil {
		t.Fatal(err)
	}
	if spaces != 1 || canvases != 1 {
		t.Errorf("%d spaces and %d canvases, want 1 and 1", spaces, canvases)
	}
	if _, _, err := service.NewAuth(pool).Login(ctx, service.SeedOwnerEmail, "staging-owner-pw"); err != nil {
		t.Errorf("seeded owner cannot log in: %v", err)
	}

	if made, err := service.SeedPlayers(ctx, pool, 3, "staging-owner-pw"); err != nil || made != 3 {
		t.Fatalf("players: made %d, err %v; want 3", made, err)
	}
	if made, err := service.SeedPlayers(ctx, pool, 3, "staging-owner-pw"); err != nil || made != 0 {
		t.Fatalf("players again: made %d, err %v; want 0", made, err)
	}
	if _, _, err := service.NewAuth(pool).Login(ctx, "player-003@example.com", "staging-owner-pw"); err != nil {
		t.Errorf("player-003 cannot log in: %v", err)
	}
	var members int
	_ = other.QueryRow(ctx, "SELECT count(*) FROM space_members").Scan(&members)
	if members != 4 {
		t.Errorf("%d members, want the owner and 3 players", members)
	}
}
