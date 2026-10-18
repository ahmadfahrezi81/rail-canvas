// Package handler: HTTP layer. Validates, calls a service, responds. Never touches the store.
package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/ratelimit"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

// Server implements the interface generated from openapi.yaml.
type Server struct {
	env        string
	auth       *service.Auth
	spaces     *service.Spaces
	canvases   *service.Canvases
	pixels     *service.Pixels
	templates  *service.Templates
	snapshots  *service.Snapshots
	timelapses *service.Timelapses
	bot        *service.Bot
	// SHA-256 of WEBHOOK_SECRET; nil: the webhook is off.
	webhookSecret *[sha256.Size]byte

	loginLimit   *ratelimit.Limiter // per email + IP: guessing one account's password
	loginIPLimit *ratelimit.Limiter // per IP: trying many accounts
	signupLimit  *ratelimit.Limiter
	joinLimit    *ratelimit.Limiter
	// per member: 10 uploads at once, then one every 6 minutes (10 an hour).
	templateLimit *ratelimit.Limiter
	// per member: 5 timelapses at once, then one every 12 minutes (5 an hour).
	timelapseLimit *ratelimit.Limiter
	// per chat account: 5 tries at once, then one every 12 seconds (guessing link codes).
	botLinkLimit *ratelimit.Limiter
	// per member: 5 codes at once, then one a minute.
	linkCodeLimit *ratelimit.Limiter
}

var _ apigen.StrictServerInterface = (*Server)(nil)

func NewServer(env string, auth *service.Auth, spaces *service.Spaces, canvases *service.Canvases, pixels *service.Pixels,
	templates *service.Templates, snapshots *service.Snapshots, timelapses *service.Timelapses, bot *service.Bot, webhookSecret string) *Server {
	var secret *[sha256.Size]byte
	if webhookSecret != "" {
		h := sha256.Sum256([]byte(webhookSecret))
		secret = &h
	}
	return &Server{
		env: env, auth: auth, spaces: spaces, canvases: canvases, pixels: pixels, templates: templates,
		snapshots: snapshots, timelapses: timelapses, bot: bot, webhookSecret: secret,
		// 5 attempts at once, then one every 12 seconds.
		loginLimit:     ratelimit.New(12*time.Second, 5),
		loginIPLimit:   ratelimit.New(3*time.Second, 20),
		signupLimit:    ratelimit.New(12*time.Second, 5),
		joinLimit:      ratelimit.New(12*time.Second, 5),
		templateLimit:  ratelimit.New(6*time.Minute, 10),
		timelapseLimit: ratelimit.New(12*time.Minute, 5),
		botLinkLimit:   ratelimit.New(12*time.Second, 5),
		linkCodeLimit:  ratelimit.New(time.Minute, 5),
	}
}

var (
	errUnauthorized = apigen.Error{Error: "unauthorized"}
	errNotFound     = apigen.Error{Error: "not found"}
	errTooMany      = apigen.Error{Error: "too many attempts, try again in a minute"}
)

// ShareLimits moves every rate limit to Redis, so all API copies count together.
func (s *Server) ShareLimits(rdb *redis.Client, onErr func(op string, err error), up func() bool) {
	s.loginLimit.Share(rdb, "login", onErr, up)
	s.loginIPLimit.Share(rdb, "login-ip", onErr, up)
	s.signupLimit.Share(rdb, "signup", onErr, up)
	s.joinLimit.Share(rdb, "join", onErr, up)
	s.templateLimit.Share(rdb, "template", onErr, up)
	s.timelapseLimit.Share(rdb, "timelapse", onErr, up)
	s.botLinkLimit.Share(rdb, "bot-link", onErr, up)
	s.linkCodeLimit.Share(rdb, "link-code", onErr, up)
}

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
