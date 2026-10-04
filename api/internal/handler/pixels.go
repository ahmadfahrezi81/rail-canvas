package handler

import (
	"context"
	"errors"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func (s *Server) PlacePixel(ctx context.Context, req apigen.PlacePixelRequestObject) (apigen.PlacePixelResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.PlacePixel401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.PlacePixel404JSONResponse(errNotFound), nil
	}

	b := req.Body
	p, err := s.pixels.Place(ctx, req.SpaceId, req.CanvasId, user.ID, b.X, b.Y, b.Color, "web")
	var cd *service.CooldownError
	switch {
	case errors.As(err, &cd):
		return apigen.PlacePixel429JSONResponse{Error: "cooldown", NextPlaceAt: cd.NextPlaceAt}, nil
	case errors.Is(err, service.ErrInvalidPixel):
		return apigen.PlacePixel400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.PlacePixel404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.PlacePixel201JSONResponse{
		PixelId: p.PixelID, X: p.X, Y: p.Y, Color: p.Color, PlacedAt: p.PlacedAt, NextPlaceAt: p.NextPlaceAt,
	}, nil
}

func (s *Server) CreateWSTicket(ctx context.Context, _ apigen.CreateWSTicketRequestObject) (apigen.CreateWSTicketResponseObject, error) {
	user, ok := middleware.UserFrom(ctx)
	if !ok {
		return apigen.CreateWSTicket401JSONResponse(errUnauthorized), nil
	}
	ticket, expires, err := s.auth.CreateWSTicket(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return apigen.CreateWSTicket201JSONResponse{Ticket: ticket, ExpiresAt: expires}, nil
}
