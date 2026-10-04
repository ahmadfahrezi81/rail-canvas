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

type Publisher interface {
	Publish(topic string, msg any)
}

type Placed struct {
	PixelID     int64
	X, Y, Color int
	PlacedAt    time.Time
	NextPlaceAt time.Time
}

type Pixels struct {
	pool *pgxpool.Pool
	pub  Publisher
}

func NewPixels(pool *pgxpool.Pool, pub Publisher) *Pixels {
	return &Pixels{pool: pool, pub: pub}
}

// Place claims the cooldown, writes history and board in one transaction, and
// announces the pixel only after the commit.
func (s *Pixels) Place(ctx context.Context, spaceID, canvasID, userID uuid.UUID, x, y, color int, source string) (Placed, error) {
	var out Placed
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
		cooldown := time.Duration(c.CooldownSeconds) * time.Second

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
	if err != nil {
		return Placed{}, err
	}
	s.pub.Publish(realtime.CanvasTopic(canvasID), realtime.PixelPlaced(canvasID, out.PixelID, x, y, color))
	return out, nil
}
