// Package httpx: response helpers. Every error body is {"error": "..."}.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Error string `json:"error"`
}

func JSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.ErrorContext(r.Context(), "encode response", "err", err)
	}
}

func OK(w http.ResponseWriter, r *http.Request, v any) {
	JSON(w, r, http.StatusOK, v)
}

func Error(w http.ResponseWriter, r *http.Request, status int, msg string) {
	JSON(w, r, status, errorBody{Error: msg})
}

func BadRequest(w http.ResponseWriter, r *http.Request, msg string) {
	Error(w, r, http.StatusBadRequest, msg)
}

func Unauthorized(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusUnauthorized, "unauthorized")
}

func Forbidden(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusForbidden, "forbidden")
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusNotFound, "not found")
}

// HandleError logs the real error and returns a generic 500.
func HandleError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.ErrorContext(r.Context(), op, "err", err)
	Error(w, r, http.StatusInternalServerError, "internal error")
}
