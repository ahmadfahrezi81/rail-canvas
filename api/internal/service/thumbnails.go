package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const thumbMaxSide = 64

// TemplateThumbKey sits beside the original, from server values only.
func TemplateThumbKey(spaceID, templateID uuid.UUID) string {
	return fmt.Sprintf("spaces/%s/templates/%s/thumb.png", spaceID, templateID)
}

// MakeThumbnail is the worker's job: a copy at most 64 × 64, or the image itself if it is
// that small already. Safe to run twice: it overwrites the same key. A template that is
// gone or not ready is skipped, not an error.
func (s *Templates) MakeThumbnail(ctx context.Context, spaceID, templateID uuid.UUID) error {
	var t store.Template
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		var err error
		t, err = q.GetTemplateByID(ctx, store.GetTemplateByIDParams{ID: templateID, SpaceID: spaceID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || t.Status != "ready" {
		return err
	}

	// Checked again before decoding: the only place an uploaded file is read whole.
	if _, _, reason, err := s.check(ctx, t.ObjectKey, int64(t.Bytes)); err != nil || reason != "" {
		return errors.Join(err, errorIf(reason))
	}
	data, err := s.objects.Get(ctx, t.ObjectKey, TemplateMaxBytes)
	if err != nil {
		return err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, shrink(img, thumbMaxSide)); err != nil {
		return err
	}
	key := TemplateThumbKey(spaceID, templateID)
	if err := s.objects.Put(ctx, key, "image/png", buf.Bytes()); err != nil {
		return err
	}
	return inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		return q.SetTemplateThumb(ctx, store.SetTemplateThumbParams{ThumbKey: pgtype.Text{String: key, Valid: true}, ID: templateID, SpaceID: spaceID})
	})
}

func errorIf(reason string) error {
	if reason == "" {
		return nil
	}
	return errors.New("template no longer passes its checks: " + reason)
}

// shrink scales down to fit side × side with nearest neighbour, so pixel art stays sharp.
// An image that already fits is returned as it is, never blown up.
func shrink(src image.Image, side int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return src
	}
	nw, nh := side, h*side/w
	if h > w {
		nw, nh = w*side/h, side
	}
	nw, nh = max(nw, 1), max(nh, 1)
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := range nh {
		for x := range nw {
			dst.Set(x, y, src.At(b.Min.X+x*w/nw, b.Min.Y+y*h/nh))
		}
	}
	return dst
}
