package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

type Canvas struct {
	ID              uuid.UUID
	Name            string
	Width, Height   int
	Palette         Palette
	CooldownSeconds int
	CreatedAt       time.Time
}

type Canvases struct {
	pool *pgxpool.Pool
}

func NewCanvases(pool *pgxpool.Pool) *Canvases {
	return &Canvases{pool: pool}
}

// DefaultCooldown matches the column default; a personal project with a small group.
const DefaultCooldown = 10

func (s *Canvases) Create(ctx context.Context, spaceID, userID uuid.UUID, name string, cooldownSeconds int) (Canvas, error) {
	var out Canvas
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		row, err := q.CreateCanvas(ctx, store.CreateCanvasParams{
			SpaceID: spaceID, Name: name, CreatedBy: uuid.NullUUID{UUID: userID, Valid: true},
			CooldownSeconds: int32(cooldownSeconds),
		})
		if err != nil {
			return err
		}
		out, err = toCanvas(row)
		return err
	})
	return out, err
}

// List: live.
func (s *Canvases) List(ctx context.Context, spaceID uuid.UUID) ([]Canvas, error) {
	var out []Canvas
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		rows, err := q.ListCanvases(ctx, spaceID)
		if err != nil {
			return err
		}
		out = make([]Canvas, 0, len(rows))
		for _, row := range rows {
			c, err := toCanvas(row)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return nil
	})
	return out, err
}

// Board returns one byte per cell, row by row. Live; Redis caches it from Step 11.
func (s *Canvases) Board(ctx context.Context, spaceID, canvasID uuid.UUID) ([]byte, error) {
	var board []byte
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		cells, err := q.ListCanvasPixels(ctx, store.ListCanvasPixelsParams{CanvasID: canvasID, SpaceID: spaceID})
		if err != nil {
			return err
		}
		w := int(c.Width)
		board = make([]byte, w*int(c.Height))
		for _, cell := range cells {
			board[int(cell.Y)*w+int(cell.X)] = byte(cell.Color)
		}
		return nil
	})
	return board, err
}

// CanSubscribe: the user is a member of the space and the canvas belongs to it.
func (s *Canvases) CanSubscribe(ctx context.Context, userID, spaceID, canvasID uuid.UUID) (bool, error) {
	if _, err := memberRole(ctx, store.New(s.pool), spaceID, userID); err != nil {
		return false, ignoreNotFound(err)
	}
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		_, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func ignoreNotFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func toCanvas(row store.Canvas) (Canvas, error) {
	p, err := paletteByID(row.Palette)
	if err != nil {
		return Canvas{}, err
	}
	return Canvas{
		ID:              row.ID,
		Name:            row.Name,
		Width:           int(row.Width),
		Height:          int(row.Height),
		Palette:         p,
		CooldownSeconds: int(row.CooldownSeconds),
		CreatedAt:       row.CreatedAt,
	}, nil
}
