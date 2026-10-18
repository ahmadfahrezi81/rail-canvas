package service

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx/v5"
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

// SeedPlayers makes player-001@example.com … player-NNN@example.com members of the seeded space,
// for the load test. Only missing ones are created; it returns how many.
func SeedPlayers(ctx context.Context, pool *pgxpool.Pool, n int, password string) (int, error) {
	q := store.New(pool)
	space, err := q.GetSpaceBySlug(ctx, SeedSpaceSlug)
	if err != nil {
		return 0, fmt.Errorf("the seeded space: %w", err)
	}
	var hash []byte // one hash for all: they share the password, and bcrypt is slow on purpose
	created := 0
	for i := 1; i <= n; i++ {
		email := fmt.Sprintf("player-%03d@example.com", i)
		user, err := q.GetUserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			if hash == nil {
				if hash, err = bcrypt.GenerateFromPassword([]byte(password), bcryptCost); err != nil {
					return created, err
				}
			}
			user, err = q.CreateUser(ctx, store.CreateUserParams{Email: email, PasswordHash: string(hash), DisplayName: fmt.Sprintf("Player %03d", i)})
			created++
		}
		if err != nil {
			return created, err
		}
		if err := q.AddMember(ctx, store.AddMemberParams{SpaceID: space.ID, UserID: user.ID, Role: "member"}); err != nil {
			return created, err
		}
	}
	return created, nil
}
