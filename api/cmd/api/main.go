// Command api: the public HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/config"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/handler"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/httpx"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(middleware.ContextHandler{
		Handler: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}),
	}).With("service", "api", "env", cfg.Env)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platform.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.DevSpaceID != uuid.Nil {
		slog.Warn("DEV_SPACE_ID set: every request acts as the dev space", "space_id", cfg.DevSpaceID)
	}

	server := handler.NewServer(cfg.Env, service.NewCanvases(pool))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           routes(cfg, server),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: WebSockets are long-lived.
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("stopped cleanly")
	return nil
}

func routes(cfg config.Config, server *handler.Server) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recover)
	r.Use(middleware.DevSpace(cfg.DevSpaceID))

	// chi's defaults are plain text.
	r.NotFound(httpx.NotFound)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusMethodNotAllowed, "method not allowed")
	})

	strict := apigen.NewStrictHandlerWithOptions(server, nil, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.BadRequest(w, r, "invalid request body")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.HandleError(w, r, r.Method+" "+r.URL.Path, err)
		},
	})
	return apigen.HandlerWithOptions(strict, apigen.ChiServerOptions{
		BaseRouter: r,
		// Bad path params, e.g. a canvasId that is not a UUID.
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.BadRequest(w, r, err.Error())
		},
	})
}
