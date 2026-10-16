package handler

import (
	"context"
	"errors"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func (s *Server) CreateTimelapse(ctx context.Context, req apigen.CreateTimelapseRequestObject) (apigen.CreateTimelapseResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.CreateTimelapse401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.CreateTimelapse404JSONResponse(errNotFound), nil
	}
	if !s.timelapseLimit.Allow(user.ID.String()) {
		return apigen.CreateTimelapse429JSONResponse{Error: "too many timelapses, try again later"}, nil
	}
	t, err := s.timelapses.Request(ctx, req.SpaceId, req.CanvasId, user.ID)
	switch {
	case errors.Is(err, service.ErrTimelapsesOff), errors.Is(err, service.ErrQueueUnavailable):
		return apigen.CreateTimelapse503JSONResponse{Error: publicError(err)}, nil
	case errors.Is(err, service.ErrTimelapsePending):
		return apigen.CreateTimelapse409JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.CreateTimelapse404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.CreateTimelapse202JSONResponse(toAPITimelapse(t)), nil
}

func (s *Server) ListTimelapses(ctx context.Context, req apigen.ListTimelapsesRequestObject) (apigen.ListTimelapsesResponseObject, error) {
	_, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.ListTimelapses401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.ListTimelapses404JSONResponse(errNotFound), nil
	}
	list, err := s.timelapses.List(ctx, req.SpaceId, req.CanvasId)
	switch {
	case errors.Is(err, service.ErrTimelapsesOff):
		return apigen.ListTimelapses503JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.ListTimelapses404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	out := apigen.ListTimelapses200JSONResponse{Timelapses: make([]apigen.Timelapse, 0, len(list))}
	for _, t := range list {
		out.Timelapses = append(out.Timelapses, toAPITimelapse(t))
	}
	return out, nil
}

func toAPITimelapse(t service.Timelapse) apigen.Timelapse {
	out := apigen.Timelapse{Id: t.ID, Status: apigen.TimelapseStatus(t.Status), CreatedAt: t.CreatedAt, FinishedAt: t.FinishedAt}
	if t.Frames > 0 {
		out.Frames = &t.Frames
	}
	if t.Error != "" {
		out.Error = &t.Error
	}
	if t.URL != "" {
		out.Url, out.UrlExpiresAt = &t.URL, &t.URLExpiresAt
	}
	return out
}

// publicError keeps an internal error joined to a sentinel out of the response.
func publicError(err error) string {
	for _, e := range []error{service.ErrQueueUnavailable, service.ErrTimelapsesOff} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return "unavailable"
}
