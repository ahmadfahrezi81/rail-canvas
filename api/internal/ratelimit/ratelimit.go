// Package ratelimit: per-key token buckets in memory. Moves to Redis in Step 11.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Limiter struct {
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

func (l *Limiter) Allow(key string) bool {
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
