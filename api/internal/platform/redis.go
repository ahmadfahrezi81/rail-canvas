package platform

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedis connects lazily: the API starts, on its fallbacks, even when Redis is down.
// Short timeouts, so a slow Redis costs a request at most a few hundred ms before the fallback runs.
func NewRedis(url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opt.DialTimeout = 500 * time.Millisecond
	opt.ReadTimeout = 200 * time.Millisecond
	opt.WriteTimeout = 200 * time.Millisecond
	opt.PoolSize = 10
	opt.MaxRetries = -1 // no retries: the caller's fallback is faster than a second try
	return redis.NewClient(opt), nil
}

// NewRedisListener is for pub/sub: one connection that waits for minutes between messages,
// so Redis's default timeouts, not the API's short ones.
func NewRedisListener(url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opt.PoolSize = 2
	return redis.NewClient(opt), nil
}
