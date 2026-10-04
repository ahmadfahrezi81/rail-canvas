// Package handler: HTTP layer. Validates, calls a service, responds. Never touches the store.
package handler

import (
	"net/http"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/httpx"
)

type healthResponse struct {
	Status string `json:"status"`
	Env    string `json:"env"`
}

// Health stays dependency-free: a DB blip must not get a healthy API restarted.
func Health(env string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, r, healthResponse{Status: "ok", Env: env})
	}
}
