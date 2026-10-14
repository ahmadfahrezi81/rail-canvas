// Package db carries the migration files inside the binary, so cli migrate needs no files on disk.
package db

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var embedded embed.FS

// Migrations is the migrations directory, rooted so goose sees the .sql files directly.
func Migrations() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic(err) // the directory is embedded at build time; it cannot be missing
	}
	return sub
}
