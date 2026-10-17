package realtime

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
)

func testRedisClient(t *testing.T) *redis.Client {
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
	return c
}

// apiCopy is one API copy: a hub, its WebSocket server and its bus. up is its breaker.
type apiCopy struct {
	hub *Hub
	url string
	bus *Bus
	up  atomic.Bool
}

func newCopy(t *testing.T, pub, listen *redis.Client) *apiCopy {
	t.Helper()
	hub, url := newServer(t)
	c := &apiCopy{hub: hub, url: url}
	c.up.Store(true)
	c.bus = NewBus(hub, uuid.NewString()).WithRedis(pub, listen, c.up.Load, func(string, error) {})
	c.bus.missedEvery = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.bus.Run(ctx)
	return c
}

// listening waits until each copy's listener is subscribed, so nothing published before is lost.
func listening(t *testing.T, copies ...*apiCopy) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, c := range copies {
		for !c.bus.subscribed.Load() {
			if time.Now().After(deadline) {
				t.Fatal("a listener did not subscribe within 5 s")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func expect(t *testing.T, c *websocket.Conn, typ apigen.WSServerMessageType, what string) apigen.WSServerMessage {
	t.Helper()
	m, err := read(c, 3*time.Second)
	if err != nil || m.Type != typ {
		t.Fatalf("%s: got %+v, %v; want %s", what, m, err, typ)
	}
	return m
}

// nothingMore reads with a deadline, which closes the socket (coder/websocket): use it last.
func nothingMore(t *testing.T, c *websocket.Conn, what string) {
	t.Helper()
	if m, err := read(c, 300*time.Millisecond); err == nil {
		t.Errorf("%s: unexpected %+v", what, m)
	}
}

func TestBusTwoCopiesDeliverOnce(t *testing.T) {
	rdb := testRedisClient(t)
	a, b := newCopy(t, rdb, testRedisClient(t)), newCopy(t, rdb, testRedisClient(t))
	listening(t, a, b)
	onA, onB := dial(t, a.url), dial(t, b.url)
	subscribe(t, onA, canvasA)
	subscribe(t, onB, canvasA)

	a.bus.Publish(space, canvasA, PixelPlaced(canvasA, 42, 1, 2, 3))
	for name, c := range map[string]*websocket.Conn{"same copy": onA, "other copy": onB} {
		if m := expect(t, c, apigen.WSServerMessageTypePixelPlaced, name); *m.PixelId != 42 {
			t.Errorf("%s: pixel %d, want 42", name, *m.PixelId)
		}
		nothingMore(t, c, name+", exactly once")
	}
	if s := b.bus.Stats(); s.Received != 1 {
		t.Errorf("copy B received %d from Redis, want 1", s.Received)
	}
}

// Redis unreachable: the publishing copy's sockets still get it, and nothing fails.
func TestBusRedisDownStillLocal(t *testing.T) {
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = dead.Close() })
	a := newCopy(t, dead, dead)
	onA := dial(t, a.url)
	subscribe(t, onA, canvasA)

	a.bus.Publish(space, canvasA, PixelPlaced(canvasA, 7, 0, 0, 0))
	expect(t, onA, apigen.WSServerMessageTypePixelPlaced, "local socket with Redis down")
}

// A pixel this copy could not announce becomes a resync of that canvas on the other copies.
func TestBusMissedAnnouncementResyncs(t *testing.T) {
	rdb := testRedisClient(t)
	a, b := newCopy(t, rdb, testRedisClient(t)), newCopy(t, rdb, testRedisClient(t))
	listening(t, a, b)
	onB, otherCanvas := dial(t, b.url), dial(t, b.url)
	subscribe(t, onB, canvasA)
	subscribe(t, otherCanvas, uuid.New())

	a.up.Store(false) // copy A's breaker is open: the announcement is skipped
	a.bus.Publish(space, canvasA, PixelPlaced(canvasA, 9, 0, 0, 0))
	time.Sleep(100 * time.Millisecond) // a retry would have run by now
	if n := b.bus.Stats().Received; n != 0 {
		t.Fatalf("copy B received %d while A could not announce, want 0", n)
	}
	a.up.Store(true)

	if m := expect(t, onB, apigen.WSServerMessageTypeResync, "after A's Redis is back"); m.CanvasId == nil || *m.CanvasId != canvasA {
		t.Errorf("resync for %v, want canvas A", m.CanvasId)
	}
	nothingMore(t, otherCanvas, "a socket on another canvas")
}

// The listener's connection dropped: its copy's sockets are told to reload.
func TestBusListenerReconnectResyncs(t *testing.T) {
	rdb := testRedisClient(t)
	listen := testRedisClient(t)
	b := newCopy(t, rdb, listen)
	listening(t, b)
	onB := dial(t, b.url)
	subscribe(t, onB, canvasA)

	if err := rdb.ClientKillByFilter(context.Background(), "TYPE", "pubsub").Err(); err != nil {
		t.Fatal(err)
	}
	if m := expect(t, onB, apigen.WSServerMessageTypeResync, "after the listener reconnected"); m.CanvasId != nil {
		t.Errorf("resync for canvas %v, want every canvas", *m.CanvasId)
	}
	if b.bus.Stats().Resyncs != 1 {
		t.Errorf("resyncs = %d, want 1", b.bus.Stats().Resyncs)
	}
}
