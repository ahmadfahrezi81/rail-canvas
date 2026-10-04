package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	h := CORS([]string{"https://rail-canvas.pages.dev", "https://*.rail-canvas.pages.dev"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)

	cases := map[string]bool{
		"https://rail-canvas.pages.dev":        true,
		"https://abc123.rail-canvas.pages.dev": true,
		"https://evil.example":                 false,
		"https://rail-canvas.pages.dev.evil":   false,
	}
	for origin, allowed := range cases {
		req := httptest.NewRequest(http.MethodGet, "/canvases", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		got := rec.Header().Get("Access-Control-Allow-Origin") == origin
		if got != allowed {
			t.Errorf("%s: allowed = %v, want %v", origin, got, allowed)
		}
	}
}
