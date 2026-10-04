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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/config"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/handler"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/httpx"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/middleware"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/realtime"
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

	auth := service.NewAuth(pool)
	canvases := service.NewCanvases(pool)
	hub := realtime.NewHub(auth, canvases, cfg.CORSOrigins)
	server := handler.NewServer(cfg.Env, auth, service.NewSpaces(pool), canvases, service.NewPixels(pool, hub))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           routes(cfg, server, auth, hub),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: WebSockets are long-lived.
	}
	srv.RegisterOnShutdown(hub.Close) // Shutdown does not close upgraded connections itself

	go logStats(ctx, hub, pool)

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

func routes(cfg config.Config, server *handler.Server, auth middleware.Authenticator, hub *realtime.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recover)
	r.Use(middleware.RealIP(cfg.RealIPHeader))
	r.Use(middleware.CORS(cfg.CORSOrigins))
	r.Use(middleware.Auth(auth))

	// chi's defaults are plain text.
	r.NotFound(httpx.NotFound)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusMethodNotAllowed, "method not allowed")
	})

	// Outside the generated interface: an upgrade hands the connection over.
	r.Get("/ws", hub.ServeWS)

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

// logStats writes the numbers later steps and the load test are read against.
func logStats(ctx context.Context, hub *realtime.Hub, pool *pgxpool.Pool) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ws, db := hub.Stats(), pool.Stat()
			slog.Info("stats",
				"ws_clients", ws.Clients,
				"ws_subscriptions", ws.Subscriptions,
				"ws_dropped_slow", ws.DroppedSlow,
				"db_in_use", db.AcquiredConns(),
				"db_idle", db.IdleConns(),
				"db_max", db.MaxConns(),
				"db_waited_total", db.EmptyAcquireCount(), // acquires that had to wait for a free connection
			)
		}
	}
}
