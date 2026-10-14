package main

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/ahmadfahrezi81/rail-canvas/api/db"
)

// Runs as postgres (the migration owner), unlike the service tests. Skips without TEST_DATABASE_URL.
func TestMigrate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	all := db.Migrations()

	if _, err := migrate(ctx, url, all); err != nil {
		t.Fatalf("first run: %v", err)
	}

	t.Run("second run applies nothing", func(t *testing.T) {
		applied, err := migrate(ctx, url, all)
		if err != nil || len(applied) != 0 {
			t.Fatalf("applied %v, err %v", applied, err)
		}
	})

	// A redeploy of an older image runs its own, shorter list of migrations.
	t.Run("an older binary applies nothing", func(t *testing.T) {
		names, err := fs.Glob(all, "*.sql")
		if err != nil || len(names) < 2 {
			t.Fatalf("found %d migrations, err %v", len(names), err)
		}
		older := fstest.MapFS{}
		for _, n := range names[:len(names)/2] {
			b, err := fs.ReadFile(all, n)
			if err != nil {
				t.Fatal(err)
			}
			older[n] = &fstest.MapFile{Data: b}
		}
		applied, err := migrate(ctx, url, older)
		if err != nil || len(applied) != 0 {
			t.Fatalf("applied %v, err %v", applied, err)
		}
	})
}
