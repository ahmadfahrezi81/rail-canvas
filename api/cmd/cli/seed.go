package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

// appLoginCmd gives the app role its password on a fresh database (staging), with no laptop.
// Where app can already log in (production) it changes nothing.
func appLoginCmd() error {
	url, password := os.Getenv("MIGRATE_DATABASE_URL"), os.Getenv("APP_DB_PASSWORD")
	if url == "" || password == "" {
		fmt.Println("app-login: MIGRATE_DATABASE_URL or APP_DB_PASSWORD not set, skipped")
		return nil
	}
	changed, err := appLogin(context.Background(), url, password)
	if err != nil {
		return fmt.Errorf("app-login: %w", err)
	}
	if changed {
		fmt.Println("app-login: app can log in now")
	} else {
		fmt.Println("app-login: app could already log in, unchanged")
	}
	return nil
}

func appLogin(ctx context.Context, url, password string) (bool, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)
	var canLogin bool
	if err := conn.QueryRow(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname = 'app'").Scan(&canLogin); err != nil {
		return false, err
	}
	if canLogin {
		return false, nil
	}
	// ALTER ROLE takes no parameters; format's %L quotes the password as a literal.
	var stmt string
	if err := conn.QueryRow(ctx, "SELECT format('ALTER ROLE app WITH LOGIN PASSWORD %L', $1::text)", password).Scan(&stmt); err != nil {
		return false, err
	}
	_, err = conn.Exec(ctx, stmt)
	return err == nil, err
}

// seedCmd fills an empty staging database. In the pre-deploy command it runs only when
// SEED_OWNER_PASSWORD is set; it never runs in production, whatever the variables say.
func seedCmd(optional bool) error {
	password := os.Getenv("SEED_OWNER_PASSWORD")
	switch {
	case os.Getenv("RAILWAY_ENVIRONMENT_NAME") == "production":
		if optional {
			return nil
		}
		return errors.New("seed: refused in production")
	case password == "" && optional:
		return nil
	case len(password) < 10 || len(password) > 72:
		return errors.New("seed: SEED_OWNER_PASSWORD must be 10 to 72 characters")
	}
	ctx := context.Background()
	pool, err := platform.NewPostgres(ctx, os.Getenv("DATABASE_URL"), platform.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	created, err := service.Seed(ctx, pool, password)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if created {
		fmt.Printf("seed: created space %s with owner %s\n", service.SeedSpaceSlug, service.SeedOwnerEmail)
	} else {
		fmt.Println("seed: users exist, nothing to do")
	}
	return nil
}
