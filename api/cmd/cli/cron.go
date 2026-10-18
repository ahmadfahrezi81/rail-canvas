package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/config"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/jobs"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

// The Railway cron services run these and exit. A run must end on its own: Railway skips
// the next run while one is still going.
const (
	snapshotTimeout = 5 * time.Minute
	cleanupTimeout  = 15 * time.Minute
	cronMaxConns    = 2
)

func cronSetup(name string, timeout time.Duration) (config.Config, context.Context, context.CancelFunc, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, nil, nil, err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).
		With("service", name, "env", cfg.Env))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return cfg, ctx, cancel, nil
}

// snapshotCmd queues this hour's snapshot pass for the worker; without Redis it takes the pass itself.
func snapshotCmd() error {
	cfg, ctx, cancel, err := cronSetup("snapshot", snapshotTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	if cfg.Bucket == nil {
		return errors.New("S3_* are required: snapshots are stored in the bucket")
	}
	now := time.Now()
	hour := now.UTC().Truncate(time.Hour)

	if cfg.RedisURL != "" {
		client, err := platform.NewRedis(cfg.RedisURL)
		if err == nil {
			defer client.Close()
			rdb := service.NewRedis(client)
			if err = jobs.NewClient(client, rdb.Up, rdb.Failed).Snapshots(ctx, now); err == nil {
				slog.Info("snapshot queued", "hour", hour)
				return nil
			}
		}
		slog.Warn("snapshot not queued, taking it here", "err", err)
	}

	pool, err := platform.NewPostgres(ctx, cfg.DatabaseURL, cronMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	objects := platform.NewBucket(platform.BucketConfig(*cfg.Bucket))
	p := service.NewSnapshots(pool, objects).Pass(ctx, now)
	slog.Info("snapshots", "hour", hour, "canvases", p.Canvases, "rendered", p.Rendered, "skipped", p.Skipped,
		"failed", p.Failed, "ms", time.Since(now).Milliseconds())
	if p.Failed > 0 {
		return errors.New("some canvases failed")
	}
	return nil
}

// cleanupCmd deletes what has expired (service.Cleanup), pinging healthchecks.io around it.
func cleanupCmd(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "count what would be deleted, delete nothing")
	_ = fs.Parse(args)

	cfg, ctx, cancel, err := cronSetup("cleanup", cleanupTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	ping := os.Getenv("HC_PING_URL") // a secret: never logged
	if ping == "" {
		slog.Warn("HC_PING_URL not set: this run is not watched")
	}
	hc(ping, "/start")

	err = func() error {
		pool, err := platform.NewPostgres(ctx, cfg.DatabaseURL, cronMaxConns)
		if err != nil {
			return err
		}
		defer pool.Close()
		var objects service.ObjectStore
		if cfg.Bucket != nil {
			objects = platform.NewBucket(platform.BucketConfig(*cfg.Bucket))
		} else {
			slog.Warn("no bucket: timelapses and templates skipped")
		}
		start := time.Now()
		n, err := service.NewCleanup(pool, objects).Run(ctx, start, *dryRun)
		slog.Info("cleanup", "dry_run", *dryRun, "sessions", n.Sessions, "tickets", n.Tickets, "invites", n.Invites, "link_codes", n.LinkCodes,
			"pixels", n.Pixels, "timelapses", n.Timelapses, "templates", n.Templates, "ms", time.Since(start).Milliseconds())
		return err
	}()
	if err != nil {
		hc(ping, "/fail")
		return err
	}
	hc(ping, "")
	return nil
}

// hc pings healthchecks.io; a failed ping is logged, never fatal. The URL is never logged.
func hc(base, suffix string) {
	if base == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+suffix, nil)
	if err != nil {
		slog.Warn("healthchecks ping failed: HC_PING_URL is not a URL")
		return
	}
	resp, err := http.DefaultClient.Do(req)
	var uerr *url.Error
	switch {
	case errors.As(err, &uerr): // its message holds the URL
		slog.Warn("healthchecks ping failed", "ping", suffix, "err", uerr.Err)
	case err != nil:
		slog.Warn("healthchecks ping failed", "ping", suffix)
	default:
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			slog.Warn("healthchecks ping failed", "ping", suffix, "status", resp.StatusCode)
		}
	}
}
