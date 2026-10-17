package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/realtime"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

var ErrInvalidPixel = errors.New("pixel out of range")

// CooldownError: too soon; NextPlaceAt is when the member may place again.
type CooldownError struct{ NextPlaceAt time.Time }

func (e *CooldownError) Error() string {
	return "cooldown: next placement at " + e.NextPlaceAt.Format(time.RFC3339)
}

// Publisher announces to every API copy's sockets (realtime.Bus).
type Publisher interface {
	Publish(spaceID, canvasID uuid.UUID, msg any)
}

type Placed struct {
	PixelID     int64
	X, Y, Color int
	PlacedAt    time.Time
	NextPlaceAt time.Time
}

type Pixels struct {
	pool  *pgxpool.Pool
	pub   Publisher
	redis *Redis // nil: the cooldown is checked in Postgres only, and no board is cached
}

func NewPixels(pool *pgxpool.Pool, pub Publisher) *Pixels {
	return &Pixels{pool: pool, pub: pub}
}

// WithRedis checks the cooldown in Redis first and keeps the cached board current.
func (s *Pixels) WithRedis(r *Redis) *Pixels {
	s.redis = r
	return s
}

// PlaceAs places for a user named by a trusted caller (the webhook). There is no session,
// whose check refuses a suspended user, so it is checked here; a suspended user reads as not found.
func (s *Pixels) PlaceAs(ctx context.Context, spaceID, canvasID, userID uuid.UUID, x, y, color int) (Placed, error) {
	active, err := store.New(s.pool).IsActiveUser(ctx, userID)
	if err != nil {
		return Placed{}, err
	}
	if !active {
		return Placed{}, ErrNotFound
	}
	return s.Place(ctx, spaceID, canvasID, userID, x, y, color, "bot")
}

// Place claims the cooldown, writes history and board in one transaction, and
// announces the pixel only after the commit. With Redis, a too-soon click is refused
// there before Postgres's claim, which stays the rule.
func (s *Pixels) Place(ctx context.Context, spaceID, canvasID, userID uuid.UUID, x, y, color int, source string) (Placed, error) {
	var (
		out            Placed
		width          int
		cooldown       time.Duration
		claimedInRedis bool
	)
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		palette, err := paletteByID(c.Palette)
		if err != nil {
			return err
		}
		if x < 0 || y < 0 || x >= int(c.Width) || y >= int(c.Height) || color < 0 || color >= len(palette.Colors) {
			return ErrInvalidPixel
		}
		cooldown = time.Duration(c.CooldownSeconds) * time.Second
		width = int(c.Width)

		if s.redis != nil {
			left, ok := s.redis.claimCooldown(ctx, spaceID, userID, c.CooldownSeconds)
			if ok && left > 0 {
				return &CooldownError{NextPlaceAt: time.Now().Add(left)}
			}
			claimedInRedis = ok
		}

		placedAt, err := q.ClaimCooldown(ctx, store.ClaimCooldownParams{SpaceID: spaceID, UserID: userID, CooldownSeconds: c.CooldownSeconds})
		if errors.Is(err, pgx.ErrNoRows) {
			last, err := q.GetLastPlacedAt(ctx, store.GetLastPlacedAtParams{SpaceID: spaceID, UserID: userID})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound // not a member
			}
			if err != nil || last == nil {
				return fmt.Errorf("cooldown claim failed: %w", err)
			}
			return &CooldownError{NextPlaceAt: last.Add(cooldown)}
		}
		if err != nil {
			return err
		}

		row, err := q.InsertPixel(ctx, store.InsertPixelParams{
			SpaceID: spaceID, CanvasID: canvasID, X: int16(x), Y: int16(y), Color: int16(color),
			UserID: uuid.NullUUID{UUID: userID, Valid: true}, Source: source,
		})
		if err != nil {
			return err
		}
		if err := q.UpsertCanvasPixel(ctx, store.UpsertCanvasPixelParams{
			CanvasID: canvasID, X: int16(x), Y: int16(y), SpaceID: spaceID, Color: int16(color), PixelID: row.ID,
		}); err != nil {
			return err
		}
		out = Placed{PixelID: row.ID, X: x, Y: y, Color: color, PlacedAt: row.PlacedAt, NextPlaceAt: placedAt.Add(cooldown)}
		return nil
	})
	var cd *CooldownError
	switch {
	case err != nil && claimedInRedis && errors.As(err, &cd):
		s.redis.matchCooldown(ctx, spaceID, userID, cd.NextPlaceAt.Add(-cooldown))
	case err != nil && claimedInRedis:
		s.redis.releaseCooldown(ctx, spaceID, userID)
	}
	if err != nil {
		return Placed{}, err
	}
	s.pub.Publish(spaceID, canvasID, realtime.PixelPlaced(canvasID, out.PixelID, x, y, color))
	s.redis.cacheCell(ctx, spaceID, canvasID, width, x, y, color, out.PixelID)
	return out, nil
}
