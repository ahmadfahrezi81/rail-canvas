package service

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

type Membership struct {
	SpaceID uuid.UUID
	Name    string
	Slug    string
	Role    string
}

type Spaces struct {
	pool *pgxpool.Pool
}

func NewSpaces(pool *pgxpool.Pool) *Spaces {
	return &Spaces{pool: pool}
}

// Mine: live.
func (s *Spaces) Mine(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := store.New(s.pool).ListUserSpaces(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Membership, 0, len(rows))
	for _, r := range rows {
		out = append(out, Membership{SpaceID: r.ID, Name: r.Name, Slug: r.Slug, Role: r.Role})
	}
	return out, nil
}

// Get returns one membership, or ErrNotFound when the user is not a member.
func (s *Spaces) Get(ctx context.Context, userID, spaceID uuid.UUID) (Membership, error) {
	all, err := s.Mine(ctx, userID)
	if err != nil {
		return Membership{}, err
	}
	for _, m := range all {
		if m.SpaceID == spaceID {
			return m, nil
		}
	}
	return Membership{}, ErrNotFound
}

// Role: live, deliberately; a removed member must lose access on the next request.
func (s *Spaces) Role(ctx context.Context, userID, spaceID uuid.UUID) (string, error) {
	return memberRole(ctx, store.New(s.pool), spaceID, userID)
}

func memberRole(ctx context.Context, q *store.Queries, spaceID, userID uuid.UUID) (string, error) {
	role, err := q.GetMemberRole(ctx, store.GetMemberRoleParams{SpaceID: spaceID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}
