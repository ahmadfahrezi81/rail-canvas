package handler

import (
	"context"
	"errors"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

var errTemplatesOff = apigen.Error{Error: service.ErrTemplatesOff.Error()}

func (s *Server) ListTemplates(ctx context.Context, req apigen.ListTemplatesRequestObject) (apigen.ListTemplatesResponseObject, error) {
	_, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.ListTemplates401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.ListTemplates404JSONResponse(errNotFound), nil
	}
	list, err := s.templates.List(ctx, req.SpaceId, req.CanvasId)
	switch {
	case errors.Is(err, service.ErrTemplatesOff):
		return apigen.ListTemplates503JSONResponse(errTemplatesOff), nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.ListTemplates404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	out := apigen.ListTemplates200JSONResponse{Templates: make([]apigen.Template, 0, len(list))}
	for _, t := range list {
		out.Templates = append(out.Templates, toAPITemplate(t))
	}
	return out, nil
}

func (s *Server) CreateTemplate(ctx context.Context, req apigen.CreateTemplateRequestObject) (apigen.CreateTemplateResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.CreateTemplate401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.CreateTemplate404JSONResponse(errNotFound), nil
	}
	if !s.templateLimit.Allow(user.ID.String()) {
		return apigen.CreateTemplate429JSONResponse{Error: "too many uploads, try again later"}, nil
	}

	b := req.Body
	up, err := s.templates.Create(ctx, req.SpaceId, req.CanvasId, user.ID, int64(b.Bytes), string(b.ContentType), b.X, b.Y)
	switch {
	case errors.Is(err, service.ErrTemplatesOff):
		return apigen.CreateTemplate503JSONResponse(errTemplatesOff), nil
	case errors.Is(err, service.ErrInvalidTemplate):
		return apigen.CreateTemplate400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrCanvasFull):
		return apigen.CreateTemplate409JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.CreateTemplate404JSONResponse{Error: "canvas not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.CreateTemplate201JSONResponse{
		TemplateId: up.TemplateID, UploadUrl: up.URL, UploadHeaders: up.Headers, ExpiresAt: up.ExpiresAt,
	}, nil
}

func (s *Server) CompleteTemplate(ctx context.Context, req apigen.CompleteTemplateRequestObject) (apigen.CompleteTemplateResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.CompleteTemplate401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.CompleteTemplate404JSONResponse(errNotFound), nil
	}
	t, err := s.templates.Complete(ctx, req.SpaceId, req.CanvasId, req.TemplateId, user.ID)
	var rej *service.RejectedError
	switch {
	case errors.Is(err, service.ErrTemplatesOff):
		return apigen.CompleteTemplate503JSONResponse(errTemplatesOff), nil
	case errors.As(err, &rej):
		return apigen.CompleteTemplate422JSONResponse{Error: rej.Reason}, nil
	case errors.Is(err, service.ErrTemplateFinished):
		return apigen.CompleteTemplate409JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.CompleteTemplate404JSONResponse{Error: "template not found"}, nil
	case err != nil:
		return nil, err
	}
	t.UploadedByName = user.DisplayName
	return apigen.CompleteTemplate200JSONResponse(toAPITemplate(t)), nil
}

func (s *Server) DeleteTemplate(ctx context.Context, req apigen.DeleteTemplateRequestObject) (apigen.DeleteTemplateResponseObject, error) {
	user, acc, err := s.member(ctx, req.SpaceId)
	switch {
	case err != nil:
		return nil, err
	case acc == noUser:
		return apigen.DeleteTemplate401JSONResponse(errUnauthorized), nil
	case acc == notMember:
		return apigen.DeleteTemplate404JSONResponse(errNotFound), nil
	}
	err = s.templates.Delete(ctx, req.SpaceId, req.CanvasId, req.TemplateId, user.ID)
	switch {
	case errors.Is(err, service.ErrTemplatesOff):
		return apigen.DeleteTemplate503JSONResponse(errTemplatesOff), nil
	case errors.Is(err, service.ErrForbidden):
		return apigen.DeleteTemplate403JSONResponse{Error: "only the uploader or the space's owner can delete a template"}, nil
	case errors.Is(err, service.ErrNotFound):
		return apigen.DeleteTemplate404JSONResponse{Error: "template not found"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.DeleteTemplate204Response{}, nil
}

func toAPITemplate(t service.Template) apigen.Template {
	out := apigen.Template{
		Id: t.ID, X: t.X, Y: t.Y, Width: t.Width, Height: t.Height,
		CreatedAt: t.CreatedAt, Url: t.URL, UrlExpiresAt: t.URLExpiresAt,
	}
	out.UploadedBy.Name = t.UploadedByName
	if t.ThumbURL != "" {
		out.ThumbUrl = &t.ThumbURL
	}
	if t.UploadedBy.Valid {
		id := t.UploadedBy.UUID
		out.UploadedBy.Id = &id
	}
	return out
}
