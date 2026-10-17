package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	busPattern     = "space:*:canvas:*"
	listenIdle     = time.Minute      // no message for this long: ping, to find a dead connection
	listenRetry    = time.Second      // after a failed receive, before the next try
	missedRetry    = 30 * time.Second // how often announcements that failed are made good
	busPublishWait = 200 * time.Millisecond
)

// envelope is what goes over Redis: who sent it, the hub topic, and the message as sent to sockets.
type envelope struct {
	From  string          `json:"from"`
	Topic string          `json:"topic"`
	Msg   json.RawMessage `json:"msg"`
}

// Bus passes messages between API copies. Publish delivers to this copy's sockets at once,
// then announces on Redis; each copy's listener passes the others' announcements to its own
// sockets and skips its own. Without Redis it is the hub alone.
type Bus struct {
	hub *Hub
	id  string

	pub    *redis.Client // the API's client, short timeouts; nil: no Redis
	listen *redis.Client // its own connection, default timeouts: it waits for minutes
	up     func() bool
	onErr  func(op string, err error)

	mu          sync.Mutex
	missed      map[string]uuid.UUID // channel → canvas whose announcement failed
	missedEvery time.Duration

	published, received, resyncs atomic.Int64
	subscribed                   atomic.Bool // the listener is confirmed; tests wait on it
}

func NewBus(hub *Hub, replicaID string) *Bus {
	return &Bus{hub: hub, id: replicaID, missed: map[string]uuid.UUID{}, missedEvery: missedRetry}
}

// WithRedis connects the bus. up and onErr are the API's Redis circuit breaker.
func (b *Bus) WithRedis(pub, listen *redis.Client, up func() bool, onErr func(op string, err error)) *Bus {
	b.pub, b.listen, b.up, b.onErr = pub, listen, up, onErr
	return b
}

// Channel is the Redis pub/sub channel of a canvas: it carries the space, like every key.
func Channel(spaceID, canvasID uuid.UUID) string {
	return "space:" + spaceID.String() + ":canvas:" + canvasID.String()
}

// Publish never fails: a failed announcement is remembered and made good with a resync.
func (b *Bus) Publish(spaceID, canvasID uuid.UUID, msg any) {
	topic := CanvasTopic(canvasID)
	b.hub.Publish(topic, msg)
	if b.pub == nil {
		return
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return // the hub has logged it
	}
	if !b.announce(Channel(spaceID, canvasID), topic, raw) {
		b.mu.Lock()
		b.missed[Channel(spaceID, canvasID)] = canvasID
		b.mu.Unlock()
	}
}

func (b *Bus) announce(channel, topic string, raw json.RawMessage) bool {
	if !b.up() {
		return false
	}
	env, _ := json.Marshal(envelope{From: b.id, Topic: topic, Msg: raw})
	ctx, cancel := context.WithTimeout(context.Background(), busPublishWait)
	defer cancel()
	if err := b.pub.Publish(ctx, channel, env).Err(); err != nil {
		b.onErr("publish "+channel, err)
		return false
	}
	b.published.Add(1)
	return true
}

// Run listens until ctx ends, and retries failed announcements as resyncs.
func (b *Bus) Run(ctx context.Context) {
	if b.pub == nil {
		return
	}
	go b.retryMissed(ctx)

	ps := b.listen.PSubscribe(ctx, busPattern)
	defer ps.Close()
	failed := false // a confirmation after a failure means a reconnect: messages may be lost
	for {
		m, err := ps.ReceiveTimeout(ctx, listenIdle)
		switch {
		case ctx.Err() != nil:
			return
		case isTimeout(err):
			if err := ps.Ping(ctx); err != nil {
				failed = true
			}
		case err != nil:
			if !failed {
				slog.Warn("realtime: lost the Redis listener, retrying", "err", err)
			}
			failed = true
			select {
			case <-ctx.Done():
				return
			case <-time.After(listenRetry):
			}
		}
		switch m := m.(type) {
		case *redis.Subscription:
			b.subscribed.Store(true)
			if failed {
				slog.Info("realtime: Redis listener back, clients told to reload")
				b.resyncs.Add(1)
				b.hub.ResyncAll()
			}
			failed = false
		case *redis.Message:
			b.receive(m.Payload)
		}
	}
}

func (b *Bus) receive(payload string) {
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil || env.From == b.id {
		return // ours were delivered locally already
	}
	b.received.Add(1)
	b.hub.deliver(env.Topic, env.Msg)
}

// retryMissed: a canvas whose announcement failed gets a resync on the other copies
// once Redis answers again, so their clients reload instead of staying behind.
func (b *Bus) retryMissed(ctx context.Context) {
	t := time.NewTicker(b.missedEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b.mu.Lock()
		missed := b.missed
		b.missed = map[string]uuid.UUID{}
		b.mu.Unlock()
		for channel, canvasID := range missed {
			raw, _ := json.Marshal(Resync(&canvasID))
			if !b.announce(channel, CanvasTopic(canvasID), raw) {
				b.mu.Lock()
				b.missed[channel] = canvasID
				b.mu.Unlock()
			}
		}
	}
}

type BusStats struct {
	Published, Received, Resyncs int64
}

func (b *Bus) Stats() BusStats {
	return BusStats{Published: b.published.Load(), Received: b.received.Load(), Resyncs: b.resyncs.Load()}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
