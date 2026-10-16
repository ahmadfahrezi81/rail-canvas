package jobs

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) (*redis.Client, string) {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	t.Cleanup(func() { _ = c.Close() })
	return c, url
}

func TestRetryDelayIsExponential(t *testing.T) {
	for n, base := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second} {
		for range 50 {
			d := RetryDelay(n, nil, nil)
			if d < base || d > base+base/5 {
				t.Fatalf("retry %d: %v, want %v to %v", n+1, d, base, base+base/5)
			}
		}
	}
}

func TestSnapshotsQueuedOncePerHour(t *testing.T) {
	rdb, _ := testRedis(t)
	ctx := context.Background()
	c := NewClient(rdb, func() bool { return true }, func(op string, err error) { t.Errorf("%s: %v", op, err) })
	hour := time.Date(2090, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(time.Now().UnixNano()%100000) * time.Hour)
	id := "snapshots:" + hour.Format("2006-01-02T15Z")
	t.Cleanup(func() { _ = asynq.NewInspectorFromRedisClient(rdb).DeleteTask("default", id) })

	for range 3 { // three API copies at the top of the same hour
		if err := c.Snapshots(ctx, hour.Add(7*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := asynq.NewInspectorFromRedisClient(rdb).GetTaskInfo("default", id)
	if err != nil || info.MaxRetry != MaxRetry {
		t.Fatalf("task %s: %+v, %v; want one, with %d retries", id, info, err, MaxRetry)
	}
}

func TestQueueSkippedWhileRedisIsDown(t *testing.T) {
	rdb, _ := testRedis(t)
	c := NewClient(rdb, func() bool { return false }, func(string, error) {})
	if err := c.Thumbnail(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Error("queued while the breaker says Redis is down")
	}
}

// End to end: the API's client queues, a worker server runs it, a failure is retried.
func TestWorkerRunsAndRetries(t *testing.T) {
	rdb, url := testRedis(t)
	opt, err := asynq.ParseRedisURI(url)
	if err != nil {
		t.Fatal(err)
	}
	const queue = "test-worker"
	var attempts atomic.Int32
	got := make(chan Thumbnail, 1)
	srv := asynq.NewServer(opt, asynq.Config{
		Concurrency: 1, Queues: map[string]int{queue: 1},
		RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return 10 * time.Millisecond },
		LogLevel:       asynq.FatalLevel,
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeThumbnail, func(ctx context.Context, task *asynq.Task) error {
		if attempts.Add(1) == 1 {
			return context.DeadlineExceeded // the first try fails
		}
		var p Thumbnail
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			return err
		}
		got <- p
		return nil
	})
	if err := srv.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)

	c := NewClient(rdb, func() bool { return true }, func(op string, err error) { t.Errorf("%s: %v", op, err) })
	want := Thumbnail{SpaceID: uuid.New(), TemplateID: uuid.New()}
	b, _ := json.Marshal(want)
	if _, err := c.c.Enqueue(asynq.NewTask(TypeThumbnail, b), asynq.Queue(queue), asynq.MaxRetry(MaxRetry)); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if p != want || attempts.Load() != 2 {
			t.Errorf("got %+v after %d attempts, want %+v after 2", p, attempts.Load(), want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the job never ran")
	}
}
