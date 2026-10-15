package handler

import (
	"context"
	"errors"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func (s *Server) ListSnapshots(ctx context.Context, req apigen.ListSnapshotsRequestObject) (apigen.ListSnapshotsResponseObject, error) {
	_, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.ListSnapshots401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.ListSnapshots404JSONResponse(errNotFound), nil
	}
	list, err := s.snapshots.List(ctx, req.SpaceId, req.CanvasId)
	switch {
	case errors.Is(err, service.ErrSnapshotsOff):
		return apigen.ListSnapshots503JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.ListSnapshots404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	out := apigen.ListSnapshots200JSONResponse{Snapshots: make([]apigen.Snapshot, 0, len(list))}
	for _, sn := range list {
		out.Snapshots = append(out.Snapshots, apigen.Snapshot{Id: sn.ID, TakenAt: sn.TakenAt, Url: sn.URL, UrlExpiresAt: sn.URLExpiresAt})
	}
	return out, nil
}
