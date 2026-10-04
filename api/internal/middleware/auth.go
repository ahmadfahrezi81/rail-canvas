package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/httpx"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (service.User, error)
}

// Auth puts the user for a valid bearer token in the context. A missing or bad
// token means no user; handlers that need one return 401.
func Auth(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerToken(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			user, err := a.Authenticate(r.Context(), token)
			if errors.Is(err, service.ErrUnauthenticated) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				httpx.HandleError(w, r, "authenticate", err)
				return
			}
			ctx := context.WithValue(r.Context(), userKey, user)
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, tokenKey, token)))
		})
	}
}

func UserFrom(ctx context.Context) (service.User, bool) {
	u, ok := ctx.Value(userKey).(service.User)
	return u, ok
}

func BearerToken(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token, ok && token != ""
}

// TokenFrom returns the authenticated request's token (for logout).
func TokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey).(string)
	return t
}
