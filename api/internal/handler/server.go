// Package handler: HTTP layer. Validates, calls a service, responds. Never touches the store.
package handler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/ratelimit"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

// Server implements the interface generated from openapi.yaml.
type Server struct {
	env      string
	auth     *service.Auth
	spaces   *service.Spaces
	canvases *service.Canvases
	pixels   *service.Pixels

	loginLimit   *ratelimit.Limiter // per email + IP: guessing one account's password
	loginIPLimit *ratelimit.Limiter // per IP: trying many accounts
	signupLimit  *ratelimit.Limiter
	joinLimit    *ratelimit.Limiter
}

var _ apigen.StrictServerInterface = (*Server)(nil)

func NewServer(env string, auth *service.Auth, spaces *service.Spaces, canvases *service.Canvases, pixels *service.Pixels) *Server {
	return &Server{
		env: env, auth: auth, spaces: spaces, canvases: canvases, pixels: pixels,
		// 5 attempts at once, then one every 12 seconds.
		loginLimit:   ratelimit.New(12*time.Second, 5),
		loginIPLimit: ratelimit.New(3*time.Second, 20),
		signupLimit:  ratelimit.New(12*time.Second, 5),
		joinLimit:    ratelimit.New(12*time.Second, 5),
	}
}

var (
	errUnauthorized = apigen.Error{Error: "unauthorized"}
	errNotFound     = apigen.Error{Error: "not found"}
	errTooMany      = apigen.Error{Error: "too many attempts, try again in a minute"}
)

// GetHealth stays dependency-free: a DB blip must not get a healthy API restarted.
func (s *Server) GetHealth(ctx context.Context, _ apigen.GetHealthRequestObject) (apigen.GetHealthResponseObject, error) {
	return apigen.GetHealth200JSONResponse{Status: "ok", Env: s.env}, nil
}

type access int

const (
	allowed access = iota
	noUser
	notMember
)

// member checks the caller belongs to the space. Not a member reads as not
// found, so strangers cannot tell whether a space exists.
func (s *Server) member(ctx context.Context, spaceID uuid.UUID) (service.User, access, error) {
	user, ok := middleware.UserFrom(ctx)
	if !ok {
		return user, noUser, nil
	}
	_, err := s.spaces.Role(ctx, user.ID, spaceID)
	if errors.Is(err, service.ErrNotFound) {
		return user, notMember, nil
	}
	return user, allowed, err
}
