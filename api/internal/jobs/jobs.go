// Package jobs: the background jobs the API queues and the worker runs, on asynq (Redis).
// Services see only the Queue interface they declare; this package never imports them.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	TypeThumbnail = "template:thumbnail"
	TypeSnapshots = "snapshots:pass"
	TypeTimelapse = "timelapse:build"

	// Owner's call: at most 3 retries, with exponential backoff (RetryDelay).
	MaxRetry   = 3
	JobTimeout = time.Minute
)

type Thumbnail struct {
	SpaceID    uuid.UUID `json:"spaceId"`
	TemplateID uuid.UUID `json:"templateId"`
}

type Snapshots struct {
	Hour time.Time `json:"hour"`
}

type Timelapse struct {
	SpaceID     uuid.UUID `json:"spaceId"`
	TimelapseID uuid.UUID `json:"timelapseId"`
}

// RetryDelay is exponential: 10 s, 20 s, 40 s, each plus up to 20% jitter so jobs that
// failed together do not all come back at once. n is the number of retries so far.
func RetryDelay(n int, _ error, _ *asynq.Task) time.Duration {
	d := 10 * time.Second << n
	return d + time.Duration(rand.Int64N(int64(d)/5+1))
}

// LastAttempt reports whether a failure now archives the job: no retry follows.
func LastAttempt(ctx context.Context) bool {
	n, ok1 := asynq.GetRetryCount(ctx)
	limit, ok2 := asynq.GetMaxRetry(ctx)
	return ok1 && ok2 && n >= limit
}

// Client queues jobs. up and onErr come from the API's Redis wrapper, so queueing shares
// its circuit breaker and its error count.
type Client struct {
	c     *asynq.Client
	up    func() bool
	onErr func(op string, err error)
}

var ErrUnavailable = errors.New("job queue unavailable")

func NewClient(rdb *redis.Client, up func() bool, onErr func(op string, err error)) *Client {
	return &Client{c: asynq.NewClientFromRedisClient(rdb), up: up, onErr: onErr}
}

func (c *Client) enqueue(ctx context.Context, typ string, payload any, opts ...asynq.Option) error {
	if !c.up() {
		return ErrUnavailable
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	opts = append([]asynq.Option{asynq.MaxRetry(MaxRetry), asynq.Timeout(JobTimeout)}, opts...)
	if _, err := c.c.EnqueueContext(ctx, asynq.NewTask(typ, b), opts...); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil // already queued (the hourly job from another API copy)
		}
		c.onErr("enqueue "+typ, err)
		return errors.Join(ErrUnavailable, err)
	}
	return nil
}

func (c *Client) Thumbnail(ctx context.Context, spaceID, templateID uuid.UUID) error {
	return c.enqueue(ctx, TypeThumbnail, Thumbnail{SpaceID: spaceID, TemplateID: templateID})
}

// Snapshots queues one pass per hour, whatever the number of API copies: the task id is
// the hour, and a finished task keeps it for two hours.
func (c *Client) Snapshots(ctx context.Context, hour time.Time) error {
	hour = hour.UTC().Truncate(time.Hour)
	return c.enqueue(ctx, TypeSnapshots, Snapshots{Hour: hour},
		asynq.TaskID("snapshots:"+hour.Format("2006-01-02T15Z")), asynq.Retention(2*time.Hour))
}

func (c *Client) Timelapse(ctx context.Context, spaceID, timelapseID uuid.UUID) error {
	return c.enqueue(ctx, TypeTimelapse, Timelapse{SpaceID: spaceID, TimelapseID: timelapseID})
}

// QueueStats is the queue's size, for the API's stats line.
type QueueStats struct{ Pending, Active, Retry, Archived int }

func Stats(rdb *redis.Client) (QueueStats, error) {
	info, err := asynq.NewInspectorFromRedisClient(rdb).GetQueueInfo("default")
	if errors.Is(err, asynq.ErrQueueNotFound) {
		return QueueStats{}, nil // nothing queued yet
	}
	if err != nil {
		return QueueStats{}, err
	}
	return QueueStats{Pending: info.Pending, Active: info.Active, Retry: info.Retry, Archived: info.Archived}, nil
}
