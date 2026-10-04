package handler

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func (s *Server) ListCanvases(ctx context.Context, req apigen.ListCanvasesRequestObject) (apigen.ListCanvasesResponseObject, error) {
	_, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.ListCanvases401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.ListCanvases404JSONResponse(errNotFound), nil
	}
	list, err := s.canvases.List(ctx, req.SpaceId)
	if err != nil {
		return nil, err
	}
	out := apigen.ListCanvases200JSONResponse{Canvases: make([]apigen.Canvas, 0, len(list))}
	for _, c := range list {
		out.Canvases = append(out.Canvases, toAPICanvas(c))
	}
	return out, nil
}

func (s *Server) CreateCanvas(ctx context.Context, req apigen.CreateCanvasRequestObject) (apigen.CreateCanvasResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.CreateCanvas401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.CreateCanvas404JSONResponse(errNotFound), nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return apigen.CreateCanvas400JSONResponse{Error: "name must be 1 to 64 characters"}, nil
	}
	cooldown := service.DefaultCooldown
	if req.Body.CooldownSeconds != nil {
		cooldown = *req.Body.CooldownSeconds
	}
	if cooldown < 1 || cooldown > 3600 {
		return apigen.CreateCanvas400JSONResponse{Error: "cooldown must be 1 to 3600 seconds"}, nil
	}
	c, err := s.canvases.Create(ctx, req.SpaceId, user.ID, name, cooldown)
	if err != nil {
		return nil, err
	}
	return apigen.CreateCanvas201JSONResponse(toAPICanvas(c)), nil
}

func (s *Server) GetCanvasBoard(ctx context.Context, req apigen.GetCanvasBoardRequestObject) (apigen.GetCanvasBoardResponseObject, error) {
	_, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.GetCanvasBoard401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.GetCanvasBoard404JSONResponse(errNotFound), nil
	}
	board, err := s.canvases.Board(ctx, req.SpaceId, req.CanvasId)
	if errors.Is(err, service.ErrNotFound) {
		return apigen.GetCanvasBoard404JSONResponse{Error: "canvas not found"}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.GetCanvasBoard200ApplicationoctetStreamResponse{
		Body:          bytes.NewReader(board),
		ContentLength: int64(len(board)),
	}, nil
}

func toAPICanvas(c service.Canvas) apigen.Canvas {
	return apigen.Canvas{
		Id:              c.ID,
		Name:            c.Name,
		Width:           c.Width,
		Height:          c.Height,
		Palette:         apigen.Palette{Id: c.Palette.ID, Colors: c.Palette.Colors},
		CooldownSeconds: c.CooldownSeconds,
		CreatedAt:       c.CreatedAt,
	}
}
