// Package platform builds clients for outside systems.
package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool sizes per process. Budget: API replicas × MaxConns + the worker + the CLI < max_connections (500).
const (
	MaxConns       = 10
	WorkerMaxConns = 4 // two jobs at a time, and the hourly pass
)

// NewPostgres opens a pool and pings it, so a bad DATABASE_URL fails at boot.
func NewPostgres(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
