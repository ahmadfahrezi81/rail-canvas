package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, token string) (service.User, error) {
	if token == "good" {
		return service.User{Email: "a@b.c"}, nil
	}
	return service.User{}, service.ErrUnauthenticated
}

func TestAuth(t *testing.T) {
	cases := map[string]bool{"": false, "Bearer good": true, "Bearer bad": false, "good": false}
	for header, want := range cases {
		var got bool
		h := Auth(fakeAuth{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, got = UserFrom(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", header)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != want {
			t.Errorf("Authorization %q: user = %v, want %v", header, got, want)
		}
	}
}

func TestRealIPIgnoresForwardedFor(t *testing.T) {
	var got string
	h := RealIP("X-Real-IP")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ClientIP(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "10.0.0.9" {
		t.Errorf("no X-Real-IP: got %q, want the TCP peer", got)
	}

	req.Header.Set("X-Real-IP", "203.0.113.7")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.7" {
		t.Errorf("X-Real-IP: got %q", got)
	}

	req.Header.Set("X-Real-IP", "not an ip")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "10.0.0.9" {
		t.Errorf("garbage X-Real-IP: got %q, want the TCP peer", got)
	}
}
