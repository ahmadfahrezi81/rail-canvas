package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// DevSpace puts DEV_SPACE_ID in the context as the caller's space. Deleted in Step 5.
func DevSpace(id uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if id == uuid.Nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithSpaceID(r.Context(), id)))
		})
	}
}

func WithSpaceID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, spaceIDKey, id)
}

// SpaceIDFrom returns the caller's space. Never take it from a request body.
func SpaceIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(spaceIDKey).(uuid.UUID)
	return id, ok
}
