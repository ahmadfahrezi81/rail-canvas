package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS lets only the listed origins (the Pages site, localhost) call the API from a browser.
func CORS(origins []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins: origins, // a "https://*.example.com" entry matches subdomains
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{RequestIDHeader},
		MaxAge:         600,
	})
}
