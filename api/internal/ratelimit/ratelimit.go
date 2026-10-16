// Package ratelimit: per-key token buckets, in Redis when shared (so every API copy
// counts together), and in memory otherwise or whenever Redis fails.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

type Limiter struct {
	shared *redis_rate.Limiter // nil: memory only
	name   string              // keys are rate:{name}:{key}
	onErr  func(op string, err error)
	up     func() bool // false: Redis is known down, use memory without trying

	mu        sync.Mutex
	keys      map[string]*entry
	every     time.Duration
	burst     int
	idleTTL   time.Duration
	lastSweep time.Time
}

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

// New allows burst attempts at once, then one per every.
func New(every time.Duration, burst int) *Limiter {
	return &Limiter{keys: map[string]*entry{}, every: every, burst: burst, idleTTL: 10 * time.Minute}
}

// Share moves the counts to Redis under name; onErr hears about each fallback, and
// while up says false, Redis is not tried at all.
func (l *Limiter) Share(rdb *redis.Client, name string, onErr func(op string, err error), up func() bool) {
	l.shared, l.name, l.onErr, l.up = redis_rate.NewLimiter(rdb), name, onErr, up
}

func (l *Limiter) Allow(key string) bool {
	if l.shared != nil && l.up() {
		res, err := l.shared.Allow(context.Background(), l.name+":"+key, redis_rate.Limit{Rate: 1, Period: l.every, Burst: l.burst})
		if err == nil {
			return res.Allowed > 0
		}
		l.onErr("rate limit", err)
	}
	return l.allowInMemory(key)
}

func (l *Limiter) allowInMemory(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e, ok := l.keys[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.keys[key] = e
	}
	e.seen = now
	l.sweep(now)
	return e.lim.AllowN(now, 1)
}

// sweep drops idle keys so the map cannot grow without bound. Runs at most once a minute.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, e := range l.keys {
		if now.Sub(e.seen) > l.idleTTL {
			delete(l.keys, k)
		}
	}
}
