package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/ahmadfahrezi81/rail-canvas/api/db"
)

// migrateCmd is Railway's pre-deploy command: it runs once per deploy, before the new version starts.
func migrateCmd() error {
	url := os.Getenv("MIGRATE_DATABASE_URL")
	if url == "" {
		return errors.New("MIGRATE_DATABASE_URL is empty (the postgres user's URL)")
	}
	applied, err := migrate(context.Background(), url, db.Migrations())
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Println("migrations: up to date")
	}
	for _, name := range applied {
		fmt.Println("migrations: applied", name)
	}
	return nil
}

// migrate applies pending migrations and returns their file names. An older binary
// (a rollback) finds nothing pending: versions it does not know are left alone.
func migrate(ctx context.Context, url string, migrations fs.FS) ([]string, error) {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// A Postgres advisory lock, so two runs at once cannot apply the same migration twice.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Source.Path)
	}
	return names, nil
}
