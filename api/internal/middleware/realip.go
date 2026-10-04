package middleware

import (
	"context"
	"net"
	"net/http"
)

// RealIP takes the client IP from one header set by Railway's edge, never from
// headers a client can send (X-Forwarded-For is appended to, not replaced).
// Locally there is no edge, so it falls back to the TCP peer.
func RealIP(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get(header)
			if net.ParseIP(ip) == nil {
				ip, _, _ = net.SplitHostPort(r.RemoteAddr)
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}
