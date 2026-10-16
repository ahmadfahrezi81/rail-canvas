// Command worker: runs the jobs the API queues in Redis (internal/jobs). Same image as the
// API, started with /app/worker. Serves no HTTP.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/config"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/jobs"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("service", "worker", "env", cfg.Env)
	slog.SetDefault(logger)

	// Every job needs the queue and the bucket: without them there is nothing to do.
	switch {
	case cfg.RedisURL == "":
		return errors.New("REDIS_URL is required: the worker takes its jobs from Redis")
	case cfg.Bucket == nil:
		return errors.New("S3_* are required: every job reads or writes the bucket")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := platform.NewPostgres(ctx, cfg.DatabaseURL, platform.WorkerMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	objects := platform.NewBucket(platform.BucketConfig(*cfg.Bucket))
	templates := service.NewTemplates(pool, objects)
	snapshots := service.NewSnapshots(pool, objects)
	timelapses := service.NewTimelapses(pool, objects, nil)

	// asynq's own Redis connection, with default timeouts: the API's are short on purpose,
	// the worker's dequeue loop is not in a hurry.
	redisOpt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("REDIS_URL: %w", err)
	}
	var done, failed atomic.Int64
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency:     2,
		RetryDelayFunc:  jobs.RetryDelay,
		ShutdownTimeout: 20 * time.Second, // a job cut off goes back in the queue
		Logger:          asynqLogger{},
		LogLevel:        asynq.WarnLevel,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
			failed.Add(1)
			n, _ := asynq.GetRetryCount(ctx)
			level := slog.LevelWarn
			if jobs.LastAttempt(ctx) {
				level = slog.LevelError // archived: no retry follows
			}
			slog.Log(ctx, level, "job failed", "type", t.Type(), "retry", n, "max_retry", jobs.MaxRetry, "err", err)
		}),
	})

	mux := asynq.NewServeMux()
	mux.Use(func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			start := time.Now()
			err := next.ProcessTask(ctx, t)
			if err == nil {
				done.Add(1)
				slog.Info("job done", "type", t.Type(), "ms", time.Since(start).Milliseconds())
			}
			return err
		})
	})
	mux.HandleFunc(jobs.TypeThumbnail, func(ctx context.Context, t *asynq.Task) error {
		var p jobs.Thumbnail
		if err := decode(t, &p); err != nil {
			return err
		}
		return templates.MakeThumbnail(ctx, p.SpaceID, p.TemplateID)
	})
	mux.HandleFunc(jobs.TypeSnapshots, func(ctx context.Context, t *asynq.Task) error {
		var p jobs.Snapshots
		if err := decode(t, &p); err != nil {
			return err
		}
		pass := snapshots.Pass(ctx, p.Hour)
		slog.Info("snapshots", "canvases", pass.Canvases, "rendered", pass.Rendered, "skipped", pass.Skipped, "failed", pass.Failed)
		if pass.Failed > 0 {
			// A retry redoes only the failed canvases: the others already claimed this hour.
			return fmt.Errorf("%d canvases failed", pass.Failed)
		}
		return nil
	})
	mux.HandleFunc(jobs.TypeTimelapse, func(ctx context.Context, t *asynq.Task) error {
		var p jobs.Timelapse
		if err := decode(t, &p); err != nil {
			return err
		}
		return timelapses.Build(ctx, p.SpaceID, p.TimelapseID, jobs.LastAttempt(ctx))
	})

	if err := srv.Start(mux); err != nil {
		return err
	}
	slog.Info("worker started", "concurrency", 2)
	go logStats(ctx, pool, &done, &failed)

	<-ctx.Done()
	slog.Info("shutting down: running jobs get 20 s, the rest stay queued")
	srv.Shutdown()
	slog.Info("stopped cleanly")
	return nil
}

// decode: a payload that cannot be read never will be, so it is not retried.
func decode(t *asynq.Task, v any) error {
	if err := json.Unmarshal(t.Payload(), v); err != nil {
		return fmt.Errorf("payload: %w: %w", err, asynq.SkipRetry)
	}
	return nil
}

func logStats(ctx context.Context, pool *pgxpool.Pool, done, failed *atomic.Int64) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			db := pool.Stat()
			slog.Info("stats", "jobs_done", done.Load(), "jobs_failed", failed.Load(),
				"db_in_use", db.AcquiredConns(), "db_idle", db.IdleConns(), "db_max", db.MaxConns())
		}
	}
}

// asynqLogger sends asynq's own messages through slog, so they are JSON like the rest.
type asynqLogger struct{}

func (asynqLogger) Debug(args ...any) { slog.Debug(fmt.Sprint(args...), "from", "asynq") }
func (asynqLogger) Info(args ...any)  { slog.Info(fmt.Sprint(args...), "from", "asynq") }
func (asynqLogger) Warn(args ...any)  { slog.Warn(fmt.Sprint(args...), "from", "asynq") }
func (asynqLogger) Error(args ...any) { slog.Error(fmt.Sprint(args...), "from", "asynq") }
func (asynqLogger) Fatal(args ...any) {
	slog.Error(fmt.Sprint(args...), "from", "asynq")
	os.Exit(1)
}
