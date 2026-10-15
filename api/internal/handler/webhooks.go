package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

// WebhookPlacePixel is for our own programs (Step 15's bot), which prove themselves with
// the shared secret instead of a session, and name the user.
func (s *Server) WebhookPlacePixel(ctx context.Context, req apigen.WebhookPlacePixelRequestObject) (apigen.WebhookPlacePixelResponseObject, error) {
	if s.webhookSecret == nil {
		return apigen.WebhookPlacePixel503JSONResponse{Error: "webhook is not configured"}, nil
	}
	if req.Params.XWebhookSecret == nil || !s.webhookSecretMatches(*req.Params.XWebhookSecret) {
		slog.WarnContext(ctx, "webhook: wrong or missing secret")
		return apigen.WebhookPlacePixel401JSONResponse(errUnauthorized), nil
	}

	b := req.Body
	p, err := s.pixels.PlaceAs(ctx, req.SpaceId, req.CanvasId, b.UserId, b.X, b.Y, b.Color)
	var cd *service.CooldownError
	switch {
	case errors.As(err, &cd):
		return apigen.WebhookPlacePixel429JSONResponse{Error: "cooldown", NextPlaceAt: cd.NextPlaceAt}, nil
	case errors.Is(err, service.ErrInvalidPixel):
		return apigen.WebhookPlacePixel400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.WebhookPlacePixel404JSONResponse{Error: "canvas or member not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.WebhookPlacePixel201JSONResponse{
		PixelId: p.PixelID, X: p.X, Y: p.Y, Color: p.Color, PlacedAt: p.PlacedAt, NextPlaceAt: p.NextPlaceAt,
	}, nil
}

// webhookSecretMatches compares hashes in constant time: neither the length nor the
// first wrong character shows in how long it takes.
func (s *Server) webhookSecretMatches(got string) bool {
	h := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(h[:], s.webhookSecret[:]) == 1
}
