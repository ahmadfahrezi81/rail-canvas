// Package service: business logic and transactions. No net/http.
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

var ErrNotFound = errors.New("not found")

// inTenant runs fn in a transaction scoped to one space. Every space-owned read
// or write goes through here, so RLS always has a tenant to check.
func inTenant(ctx context.Context, pool *pgxpool.Pool, spaceID uuid.UUID, fn func(q *store.Queries) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.SetTenant(ctx, spaceID.String()); err != nil {
			return err
		}
		return fn(q)
	})
}
