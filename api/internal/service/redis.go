package service

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Redis holds copies and short-lived state only: Postgres stays the source of truth.
// A nil *Redis means no Redis, and every caller takes its fallback; so does every
// failed call, which is counted and logged at most once a minute.
type Redis struct {
	c           *redis.Client
	errors      atomic.Int64
	lastLog     atomic.Int64 // unix seconds of the last failure log line
	downUntil   atomic.Int64 // unix ms; until then Redis is skipped (the circuit breaker)
	stale       sync.Map     // board keys that missed a placement; dropped once Redis answers
	boardHits   atomic.Int64
	boardMisses atomic.Int64
}

func NewRedis(c *redis.Client) *Redis {
	if c == nil {
		return nil
	}
	return &Redis{c: c}
}

// Client is for the rate limiters, which keep their own fallback.
func (r *Redis) Client() *redis.Client { return r.c }

// breakFor is how long Redis is skipped after a failure: while it is down, every call
// would otherwise wait for its own timeout before falling back.
const breakFor = 5 * time.Second

// Up is false for a few seconds after a failure; callers then go straight to the fallback.
func (r *Redis) Up() bool { return time.Now().UnixMilli() >= r.downUntil.Load() }

// Failed records a call that fell back. redis.Nil (a missing key) is not a failure.
func (r *Redis) Failed(op string, err error) {
	r.downUntil.Store(time.Now().Add(breakFor).UnixMilli())
	r.errors.Add(1)
	now := time.Now().Unix()
	if last := r.lastLog.Load(); now-last >= 60 && r.lastLog.CompareAndSwap(last, now) {
		slog.Warn("redis unavailable, using fallbacks", "op", op, "err", err, "errors_total", r.errors.Load())
	}
}

type RedisStats struct {
	OK                             bool
	Errors, BoardHits, BoardMisses int64
}

// Stats pings Redis; for the 30-second stats line.
func (r *Redis) Stats(ctx context.Context) RedisStats {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ok := r.c.Ping(ctx).Err() == nil
	if ok {
		r.downUntil.Store(0)
		r.dropStale(ctx)
	}
	return RedisStats{
		OK:          ok,
		Errors:      r.errors.Load(),
		BoardHits:   r.boardHits.Load(),
		BoardMisses: r.boardMisses.Load(),
	}
}

// dropStale deletes boards that missed a placement while Redis was unreachable.
func (r *Redis) dropStale(ctx context.Context) {
	r.stale.Range(func(k, _ any) bool {
		if r.c.Del(ctx, k.(string)).Err() == nil {
			r.stale.Delete(k)
		}
		return true
	})
}

// Every key outside Postgres carries the space, except those that belong to a person.
func boardKey(spaceID, canvasID uuid.UUID) string {
	return "space:" + spaceID.String() + ":canvas:" + canvasID.String() + ":board"
}
func verKey(spaceID, canvasID uuid.UUID) string {
	return "space:" + spaceID.String() + ":canvas:" + canvasID.String() + ":ver"
}
func lockKey(spaceID, canvasID uuid.UUID) string {
	return "space:" + spaceID.String() + ":canvas:" + canvasID.String() + ":lock"
}
func cooldownKey(spaceID, userID uuid.UUID) string {
	return "space:" + spaceID.String() + ":cooldown:" + userID.String()
}
func ticketKey(hash []byte) string { return "ticket:" + hex.EncodeToString(hash) }

func isMiss(err error) bool { return errors.Is(err, redis.Nil) }
