package handler

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func (s *Server) Login(ctx context.Context, req apigen.LoginRequestObject) (apigen.LoginResponseObject, error) {
	email := service.NormalizeEmail(req.Body.Email)
	ip := middleware.ClientIP(ctx)
	if !s.loginIPLimit.Allow(ip) || !s.loginLimit.Allow(email+"|"+ip) {
		return apigen.Login429JSONResponse(errTooMany), nil
	}
	if email == "" || req.Body.Password == "" {
		return apigen.Login400JSONResponse{Error: "email and password are required"}, nil
	}
	token, user, err := s.auth.Login(ctx, email, req.Body.Password)
	if errors.Is(err, service.ErrInvalidCredentials) {
		return apigen.Login401JSONResponse{Error: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}
	return apigen.Login200JSONResponse{Token: token, User: toAPIUser(user)}, nil
}

func (s *Server) Signup(ctx context.Context, req apigen.SignupRequestObject) (apigen.SignupResponseObject, error) {
	if !s.signupLimit.Allow("signup:" + middleware.ClientIP(ctx)) {
		return apigen.Signup429JSONResponse(errTooMany), nil
	}
	b := req.Body
	name := strings.TrimSpace(b.DisplayName)
	nameLen := utf8.RuneCountInString(name)
	switch {
	case !validEmail(b.Email):
		return apigen.Signup400JSONResponse{Error: "enter a valid email"}, nil
	case nameLen < 1 || nameLen > 32:
		return apigen.Signup400JSONResponse{Error: "name must be 1 to 32 characters"}, nil
	case len(b.Password) < 10 || len(b.Password) > 72: // bcrypt ignores bytes past 72
		return apigen.Signup400JSONResponse{Error: "password must be 10 to 72 characters"}, nil
	}

	token, user, err := s.auth.Signup(ctx, service.SignupParams{
		Code: b.Code, Email: b.Email, DisplayName: name, Password: b.Password,
	})
	switch {
	case errors.Is(err, service.ErrInvalidInvite):
		return apigen.Signup400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, service.ErrEmailTaken):
		return apigen.Signup409JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	return apigen.Signup201JSONResponse{Token: token, User: toAPIUser(user)}, nil
}

func (s *Server) Logout(ctx context.Context, _ apigen.LogoutRequestObject) (apigen.LogoutResponseObject, error) {
	if _, ok := middleware.UserFrom(ctx); !ok {
		return apigen.Logout401JSONResponse(errUnauthorized), nil
	}
	if err := s.auth.Logout(ctx, middleware.TokenFrom(ctx)); err != nil {
		return nil, err
	}
	return apigen.Logout204Response{}, nil
}

func (s *Server) GetMe(ctx context.Context, _ apigen.GetMeRequestObject) (apigen.GetMeResponseObject, error) {
	user, ok := middleware.UserFrom(ctx)
	if !ok {
		return apigen.GetMe401JSONResponse(errUnauthorized), nil
	}
	spaces, err := s.spaces.Mine(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := apigen.GetMe200JSONResponse{User: toAPIUser(user), Spaces: make([]apigen.SpaceMembership, 0, len(spaces))}
	for _, m := range spaces {
		out.Spaces = append(out.Spaces, toAPIMembership(m))
	}
	return out, nil
}

func (s *Server) JoinSpace(ctx context.Context, req apigen.JoinSpaceRequestObject) (apigen.JoinSpaceResponseObject, error) {
	user, ok := middleware.UserFrom(ctx)
	if !ok {
		return apigen.JoinSpace401JSONResponse(errUnauthorized), nil
	}
	if !s.joinLimit.Allow("join:" + middleware.ClientIP(ctx)) {
		return apigen.JoinSpace429JSONResponse(errTooMany), nil
	}
	spaceID, err := s.auth.Join(ctx, user.ID, req.Body.Code)
	if errors.Is(err, service.ErrInvalidInvite) {
		return apigen.JoinSpace400JSONResponse{Error: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := s.spaces.Get(ctx, user.ID, spaceID)
	if err != nil {
		return nil, err
	}
	return apigen.JoinSpace200JSONResponse(toAPIMembership(m)), nil
}

func (s *Server) CreateInvite(ctx context.Context, req apigen.CreateInviteRequestObject) (apigen.CreateInviteResponseObject, error) {
	user, ok := middleware.UserFrom(ctx)
	if !ok {
		return apigen.CreateInvite401JSONResponse(errUnauthorized), nil
	}
	code, expires, err := s.auth.CreateInvite(ctx, user.ID, req.SpaceId)
	switch {
	case errors.Is(err, service.ErrNotFound):
		return apigen.CreateInvite404JSONResponse(errNotFound), nil
	case errors.Is(err, service.ErrForbidden):
		return apigen.CreateInvite403JSONResponse{Error: "only owners can invite"}, nil
	case err != nil:
		return nil, err
	}
	return apigen.CreateInvite201JSONResponse{Code: code, ExpiresAt: expires}, nil
}

func validEmail(email string) bool {
	e := service.NormalizeEmail(email)
	at := strings.IndexByte(e, '@')
	return len(e) >= 3 && len(e) <= 254 && at > 0 && at < len(e)-1
}

func toAPIUser(u service.User) apigen.User {
	return apigen.User{Id: u.ID, Email: u.Email, DisplayName: u.DisplayName}
}

func toAPIMembership(m service.Membership) apigen.SpaceMembership {
	return apigen.SpaceMembership{Id: m.SpaceID, Name: m.Name, Slug: m.Slug, Role: apigen.SpaceMembershipRole(m.Role)}
}
