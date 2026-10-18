package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

// The seeded space's owner, for staging and the staging tests. Never production (cli refuses).
const (
	SeedSpaceSlug  = "staging-club"
	SeedOwnerEmail = "staging-owner@example.com"
)

// Seed fills an empty database: a space, its owner and a canvas. False when users already exist.
func Seed(ctx context.Context, pool *pgxpool.Pool, ownerPassword string) (bool, error) {
	spaceID, err := NewAuth(pool).Bootstrap(ctx, BootstrapParams{
		SpaceName: "Staging Club", SpaceSlug: SeedSpaceSlug, Email: SeedOwnerEmail,
		DisplayName: "Staging Owner", Password: ownerPassword,
	})
	if errors.Is(err, ErrAlreadyBootstrap) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	owner, err := store.New(pool).GetUserByEmail(ctx, SeedOwnerEmail)
	if err != nil {
		return false, err
	}
	_, err = NewCanvases(pool).Create(ctx, spaceID, owner.ID, "Staging canvas", 10)
	return true, err
}
