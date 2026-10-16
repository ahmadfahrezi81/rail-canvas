package ratelimit

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestBurstThenRefuse(t *testing.T) {
	l := New(time.Minute, 5)
	for i := range 5 {
		if !l.Allow("a") {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	if l.Allow("a") {
		t.Error("6th attempt allowed, want refused")
	}
	if !l.Allow("b") {
		t.Error("another key was refused")
	}
}

func redisClient(t *testing.T, url string) *redis.Client {
	t.Helper()
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opt.DialTimeout, opt.ReadTimeout, opt.MaxRetries = 200*time.Millisecond, 200*time.Millisecond, -1
	c := redis.NewClient(opt)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Two API copies sharing one Redis share one count.
func TestSharedAcrossCopies(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	name := "test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	a, b := New(time.Minute, 4), New(time.Minute, 4)
	a.Share(redisClient(t, url), name, func(string, error) { t.Error("unexpected fallback") }, func() bool { return true })
	b.Share(redisClient(t, url), name, func(string, error) { t.Error("unexpected fallback") }, func() bool { return true })
	for i := range 4 {
		l := a
		if i%2 == 1 {
			l = b
		}
		if !l.Allow("k") {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	if a.Allow("k") || b.Allow("k") {
		t.Error("5th attempt allowed on a copy, want refused: the count is shared")
	}
}

func TestRedisDownFallsBackToMemory(t *testing.T) {
	l := New(time.Minute, 2)
	fallbacks := 0
	l.Share(redisClient(t, "redis://127.0.0.1:1"), "down", func(string, error) { fallbacks++ }, func() bool { return true })
	if !l.Allow("k") || !l.Allow("k") || l.Allow("k") {
		t.Error("in-memory limit not applied while Redis is down")
	}
	if fallbacks != 3 {
		t.Errorf("%d fallbacks reported, want 3", fallbacks)
	}
}
