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

// The bot endpoints are for our own Function, which proves itself with the shared secret
// instead of a session. Who the player is comes from bot_links, never from the caller.

func (s *Server) WebhookBotLink(ctx context.Context, req apigen.WebhookBotLinkRequestObject) (apigen.WebhookBotLinkResponseObject, error) {
	switch s.webhookCheck(ctx, req.Params.XWebhookSecret) {
	case webhookOff:
		return apigen.WebhookBotLink503JSONResponse{Error: "webhook is not configured"}, nil
	case webhookDenied:
		return apigen.WebhookBotLink401JSONResponse(errUnauthorized), nil
	}
	b := req.Body
	if b.Platform != apigen.Telegram || b.PlatformUserId == "" || len(b.PlatformUserId) > 64 {
		return apigen.WebhookBotLink400JSONResponse{Error: "unknown platform or user"}, nil
	}
	if !s.botLinkLimit.Allow(string(b.Platform) + ":" + b.PlatformUserId) {
		return apigen.WebhookBotLink429JSONResponse(errTooMany), nil
	}
	l, err := s.bot.Link(ctx, string(b.Platform), b.PlatformUserId, b.Code)
	switch {
	case errors.Is(err, service.ErrInvalidLinkCode):
		return apigen.WebhookBotLink404JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	return apigen.WebhookBotLink200JSONResponse{DisplayName: l.DisplayName, CanvasName: l.CanvasName}, nil
}

func (s *Server) WebhookBotPlacePixel(ctx context.Context, req apigen.WebhookBotPlacePixelRequestObject) (apigen.WebhookBotPlacePixelResponseObject, error) {
	switch s.webhookCheck(ctx, req.Params.XWebhookSecret) {
	case webhookOff:
		return apigen.WebhookBotPlacePixel503JSONResponse{Error: "webhook is not configured"}, nil
	case webhookDenied:
		return apigen.WebhookBotPlacePixel401JSONResponse(errUnauthorized), nil
	}
	b := req.Body
	if b.Platform != apigen.Telegram || b.PlatformUserId == "" {
		return apigen.WebhookBotPlacePixel400JSONResponse{Error: "unknown platform or user"}, nil
	}
	p, err := s.bot.Place(ctx, string(b.Platform), b.PlatformUserId, b.X, b.Y, b.Color)
	var cd *service.CooldownError
	switch {
	case errors.As(err, &cd):
		return apigen.WebhookBotPlacePixel429JSONResponse{Error: "cooldown", NextPlaceAt: cd.NextPlaceAt}, nil
	case errors.Is(err, service.ErrInvalidPixel), errors.Is(err, service.ErrUnknownColor):
		return apigen.WebhookBotPlacePixel400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotLinked):
		return apigen.WebhookBotPlacePixel404JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.WebhookBotPlacePixel404JSONResponse{Error: "canvas or member not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.WebhookBotPlacePixel201JSONResponse{
		PixelId: p.PixelID, X: p.X, Y: p.Y, Color: p.Color, ColorName: p.ColorName, NextPlaceAt: p.NextPlaceAt,
	}, nil
}

type webhookAccess int

const (
	webhookAllowed webhookAccess = iota
	webhookOff
	webhookDenied
)

func (s *Server) webhookCheck(ctx context.Context, got *string) webhookAccess {
	switch {
	case s.webhookSecret == nil:
		return webhookOff
	case got == nil || !s.webhookSecretMatches(*got):
		slog.WarnContext(ctx, "webhook: wrong or missing secret")
		return webhookDenied
	}
	return webhookAllowed
}

// webhookSecretMatches compares hashes in constant time: neither the length nor the
// first wrong character shows in how long it takes.
func (s *Server) webhookSecretMatches(got string) bool {
	h := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(h[:], s.webhookSecret[:]) == 1
}

func (s *Server) CreateBotLinkCode(ctx context.Context, req apigen.CreateBotLinkCodeRequestObject) (apigen.CreateBotLinkCodeResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.CreateBotLinkCode401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.CreateBotLinkCode404JSONResponse(errNotFound), nil
	}
	if !s.linkCodeLimit.Allow(user.ID.String()) {
		return apigen.CreateBotLinkCode429JSONResponse(errTooMany), nil
	}
	code, expires, err := s.bot.MakeLinkCode(ctx, user.ID, req.SpaceId, req.CanvasId)
	switch {
	case errors.Is(err, service.ErrNotFound):
		return apigen.CreateBotLinkCode404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.CreateBotLinkCode201JSONResponse{Code: code, ExpiresAt: expires}, nil
}
