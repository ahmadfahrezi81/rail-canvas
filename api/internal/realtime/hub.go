// Package realtime: the in-process WebSocket hub. One topic per canvas.
// Behind Publish, so Step 14 can fan out through Redis pub/sub instead.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/httpx"
)

const (
	sendBuffer   = 64 // messages queued per client before it counts as too slow
	maxRead      = 1024
	pingEvery    = 30 * time.Second
	writeTimeout = 10 * time.Second
)

type TicketRedeemer interface {
	RedeemTicket(ctx context.Context, ticket string) (uuid.UUID, error)
}

type SubscribeChecker interface {
	// CanSubscribe: the user is a member of the space and the canvas is in it.
	CanSubscribe(ctx context.Context, userID, spaceID, canvasID uuid.UUID) (bool, error)
}

type Hub struct {
	tickets TicketRedeemer
	access  SubscribeChecker
	origins []string

	mu      sync.Mutex
	clients map[*client]struct{}
	topics  map[string]map[*client]struct{}

	droppedSlow atomic.Int64
}

func NewHub(tickets TicketRedeemer, access SubscribeChecker, corsOrigins []string) *Hub {
	return &Hub{
		tickets: tickets,
		access:  access,
		origins: originPatterns(corsOrigins),
		clients: map[*client]struct{}{},
		topics:  map[string]map[*client]struct{}{},
	}
}

func CanvasTopic(canvasID uuid.UUID) string { return "canvas:" + canvasID.String() }

func PixelPlaced(canvasID uuid.UUID, pixelID int64, x, y, color int) apigen.WSServerMessage {
	return apigen.WSServerMessage{
		Type: apigen.WSServerMessageTypePixelPlaced, CanvasId: &canvasID, PixelId: &pixelID, X: &x, Y: &y, Color: &color,
	}
}

// Publish never blocks: a client whose buffer is full is disconnected instead.
func (h *Hub) Publish(topic string, msg any) {
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("realtime: marshal", "err", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.topics[topic] {
		select {
		case c.send <- b:
		default:
			if c.slow.CompareAndSwap(false, true) { // count and close once
				h.droppedSlow.Add(1)
				go c.close(websocket.StatusPolicyViolation, "too slow")
			}
		}
	}
}

type Stats struct {
	Clients, Subscriptions int
	DroppedSlow            int64
}

func (h *Hub) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := 0
	for _, cs := range h.topics {
		subs += len(cs)
	}
	return Stats{Clients: len(h.clients), Subscriptions: subs, DroppedSlow: h.droppedSlow.Load()}
}

// Close says "going away" to every client, so they reconnect to the next deploy.
// http.Server.Shutdown does not do this: upgraded connections are no longer its own.
func (h *Hub) Close() {
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	// In parallel: each close may wait for the peer's reply.
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.close(websocket.StatusGoingAway, "server restarting")
		}()
	}
	wg.Wait()
}

// ServeWS: GET /ws?ticket=…
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	userID, err := h.tickets.RedeemTicket(r.Context(), r.URL.Query().Get("ticket"))
	if err != nil {
		httpx.Unauthorized(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.origins})
	if err != nil {
		return // Accept has already written the response
	}
	conn.SetReadLimit(maxRead)

	// Detached from the request: the socket outlives the handler's usual deadlines.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	c := &client{conn: conn, userID: userID, send: make(chan []byte, sendBuffer), cancel: cancel}
	h.add(c)
	defer h.remove(c)

	go c.writeLoop(ctx)
	h.readLoop(ctx, c)
}

func (h *Hub) readLoop(ctx context.Context, c *client) {
	defer c.close(websocket.StatusNormalClosure, "")
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var msg apigen.WSClientMessage
		if json.Unmarshal(data, &msg) != nil {
			c.reply(apigen.WSServerMessage{Type: apigen.WSServerMessageTypeError, Error: ptr("bad message")})
			continue
		}
		topic := CanvasTopic(msg.CanvasId)
		switch msg.Type {
		case apigen.Subscribe:
			ok, err := h.access.CanSubscribe(ctx, c.userID, msg.SpaceId, msg.CanvasId)
			if err != nil {
				slog.ErrorContext(ctx, "realtime: subscribe check", "err", err)
			}
			if !ok {
				c.reply(apigen.WSServerMessage{Type: apigen.WSServerMessageTypeError, CanvasId: &msg.CanvasId, Error: ptr("not found")})
				continue
			}
			h.subscribe(c, topic)
			c.reply(apigen.WSServerMessage{Type: apigen.WSServerMessageTypeSubscribed, CanvasId: &msg.CanvasId})
		case apigen.Unsubscribe:
			h.unsubscribe(c, topic)
		}
	}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	for topic, cs := range h.topics {
		delete(cs, c)
		if len(cs) == 0 {
			delete(h.topics, topic)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) subscribe(c *client, topic string) {
	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = map[*client]struct{}{}
	}
	h.topics[topic][c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unsubscribe(c *client, topic string) {
	h.mu.Lock()
	delete(h.topics[topic], c)
	h.mu.Unlock()
}

type client struct {
	conn      *websocket.Conn
	userID    uuid.UUID
	send      chan []byte
	cancel    context.CancelFunc
	closeOnce sync.Once
	slow      atomic.Bool
}

// reply queues a direct message; a full buffer drops it rather than blocking the reader.
func (c *client) reply(msg apigen.WSServerMessage) {
	b, _ := json.Marshal(msg)
	select {
	case c.send <- b:
	default:
	}
}

func (c *client) writeLoop(ctx context.Context) {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.send:
			if err := c.write(ctx, b); err != nil {
				c.close(websocket.StatusInternalError, "write failed")
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pctx) // needs the read loop running to see the pong
			cancel()
			if err != nil {
				c.close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

func (c *client) write(ctx context.Context, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, b)
}

func (c *client) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		// Close frame first; cancelling first would make the library drop the socket without it.
		if err := c.conn.Close(code, reason); err != nil {
			_ = c.conn.CloseNow()
		}
		c.cancel()
	})
}

// originPatterns turns CORS origins ("https://x.pages.dev") into the host
// patterns websocket.Accept matches against ("x.pages.dev").
func originPatterns(corsOrigins []string) []string {
	out := make([]string, 0, len(corsOrigins))
	for _, o := range corsOrigins {
		o = strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://")
		out = append(out, o)
	}
	return out
}

func ptr[T any](v T) *T { return &v }
