package service

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	linkCodeTTL = 10 * time.Minute
	// No look-alikes (0/O, 1/I/L): 31 characters, 8 of them is about 40 bits.
	linkCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	linkCodeLen      = 8
)

var (
	ErrInvalidLinkCode = errors.New("invalid, used or expired link code")
	ErrNotLinked       = errors.New("this chat account is not linked")
	ErrUnknownColor    = errors.New("unknown colour")
)

// Bot ties chat accounts (Telegram) to members, and places pixels for them. The chat
// platform's identity is never trusted alone: a member makes a code, the chat account sends it.
type Bot struct {
	pool   *pgxpool.Pool
	pixels *Pixels
}

func NewBot(pool *pgxpool.Pool, pixels *Pixels) *Bot {
	return &Bot{pool: pool, pixels: pixels}
}

// MakeLinkCode is for a member, from a canvas page. The code is returned once; only its hash is stored.
func (b *Bot) MakeLinkCode(ctx context.Context, userID, spaceID, canvasID uuid.UUID) (string, time.Time, error) {
	code := newLinkCode()
	var expires time.Time
	err := inTenant(ctx, b.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		expires, err = q.CreateLinkCode(ctx, store.CreateLinkCodeParams{
			CodeHash: hashSecret(normalizeCode(code)), UserID: userID, SpaceID: spaceID, CanvasID: canvasID,
			ExpiresAt: time.Now().Add(linkCodeTTL),
		})
		return err
	})
	return code[:4] + "-" + code[4:], expires, err
}

type BotLinked struct {
	DisplayName, CanvasName string
}

// Link redeems a code for a chat account. A new link replaces the account's old one.
func (b *Bot) Link(ctx context.Context, platform, platformUserID, code string) (BotLinked, error) {
	var out BotLinked
	var spaceID, canvasID uuid.UUID
	err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.RedeemLinkCode(ctx, hashSecret(normalizeCode(code)))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidLinkCode
		}
		if err != nil {
			return err
		}
		out.DisplayName, spaceID, canvasID = row.DisplayName, row.SpaceID, row.CanvasID
		return q.UpsertBotLink(ctx, store.UpsertBotLinkParams{
			Platform: platform, PlatformUserID: platformUserID, UserID: row.UserID, SpaceID: row.SpaceID, CanvasID: row.CanvasID,
		})
	})
	if err != nil {
		return BotLinked{}, err
	}
	err = inTenant(ctx, b.pool, spaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		out.CanvasName = c.Name
		return err
	})
	return out, err
}

type BotPlaced struct {
	Placed
	ColorName string
}

// Place draws for a linked chat account on its linked canvas, as that member: same
// cooldown, membership and active checks as the browser (Pixels.PlaceAs).
func (b *Bot) Place(ctx context.Context, platform, platformUserID string, x, y int, color string) (BotPlaced, error) {
	link, err := store.New(b.pool).GetBotLink(ctx, store.GetBotLinkParams{Platform: platform, PlatformUserID: platformUserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return BotPlaced{}, ErrNotLinked
	}
	if err != nil {
		return BotPlaced{}, err
	}
	var palette Palette
	err = inTenant(ctx, b.pool, link.SpaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: link.CanvasID, SpaceID: link.SpaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		palette, err = paletteByID(c.Palette)
		return err
	})
	if err != nil {
		return BotPlaced{}, err
	}
	index, ok := palette.Color(color)
	if !ok {
		return BotPlaced{}, ErrUnknownColor
	}
	p, err := b.pixels.PlaceAs(ctx, link.SpaceID, link.CanvasID, link.UserID, x, y, index)
	if err != nil {
		return BotPlaced{}, err
	}
	return BotPlaced{Placed: p, ColorName: palette.Names[index]}, nil
}

// newLinkCode draws bytes below 248 (8 × 31) only, so every character is equally likely.
func newLinkCode() string {
	out := make([]byte, 0, linkCodeLen)
	b := make([]byte, 1)
	for len(out) < linkCodeLen {
		_, _ = rand.Read(b)
		if b[0] < 248 {
			out = append(out, linkCodeAlphabet[int(b[0])%len(linkCodeAlphabet)])
		}
	}
	return string(out)
}
