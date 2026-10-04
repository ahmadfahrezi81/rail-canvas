package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
)

var (
	space    = uuid.New()
	canvasA  = uuid.New()
	canvasB  = uuid.New() // the test user may not subscribe to this one
	testUser = uuid.New()
)

type fakeTickets struct{}

func (fakeTickets) RedeemTicket(_ context.Context, t string) (uuid.UUID, error) {
	if t == "good" {
		return testUser, nil
	}
	return uuid.Nil, errors.New("bad ticket")
}

type fakeAccess struct{}

func (fakeAccess) CanSubscribe(_ context.Context, _, _, canvasID uuid.UUID) (bool, error) {
	return canvasID != canvasB, nil
}

func newServer(t *testing.T) (*Hub, string) {
	hub := NewHub(fakeTickets{}, fakeAccess{}, nil)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?ticket="
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url+"good", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func send(t *testing.T, c *websocket.Conn, typ apigen.WSClientMessageType, canvas uuid.UUID) {
	t.Helper()
	b, _ := json.Marshal(apigen.WSClientMessage{Type: typ, SpaceId: space, CanvasId: canvas})
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func read(c *websocket.Conn, wait time.Duration) (apigen.WSServerMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, b, err := c.Read(ctx)
	var m apigen.WSServerMessage
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	return m, err
}

func subscribe(t *testing.T, c *websocket.Conn, canvas uuid.UUID) apigen.WSServerMessage {
	t.Helper()
	send(t, c, apigen.Subscribe, canvas)
	m, err := read(c, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBadTicketIsRefused(t *testing.T) {
	_, url := newServer(t)
	_, res, err := websocket.Dial(context.Background(), url+"nope", nil)
	if err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, status = %v; want a 401", err, res)
	}
}

func TestDeliveryAndIsolation(t *testing.T) {
	hub, url := newServer(t)
	a1, a2, other, denied := dial(t, url), dial(t, url), dial(t, url), dial(t, url)

	for _, c := range []*websocket.Conn{a1, a2} {
		if m := subscribe(t, c, canvasA); m.Type != apigen.WSServerMessageTypeSubscribed {
			t.Fatalf("subscribe: got %+v", m)
		}
	}
	subscribe(t, other, uuid.New())
	if m := subscribe(t, denied, canvasB); m.Type != apigen.WSServerMessageTypeError {
		t.Errorf("forbidden subscribe: got %+v, want an error", m)
	}

	if s := hub.Stats(); s.Clients != 4 || s.Subscriptions != 3 {
		t.Errorf("stats = %+v, want 4 clients, 3 subscriptions", s)
	}

	hub.Publish(CanvasTopic(canvasA), PixelPlaced(canvasA, 42, 1, 2, 3))

	for i, c := range []*websocket.Conn{a1, a2} {
		m, err := read(c, 2*time.Second)
		if err != nil || m.Type != apigen.WSServerMessageTypePixelPlaced || *m.PixelId != 42 {
			t.Errorf("client %d: got %+v, %v", i, m, err)
		}
	}
	// Last: a timed-out read closes the connection (the library's rule).
	for name, c := range map[string]*websocket.Conn{"other canvas": other, "denied": denied} {
		if m, err := read(c, 200*time.Millisecond); err == nil {
			t.Errorf("%s received %+v, want nothing", name, m)
		}
	}
}

func TestSlowClientIsDroppedOthersContinue(t *testing.T) {
	hub, url := newServer(t)
	fast, slow := dial(t, url), dial(t, url)
	subscribe(t, fast, canvasA)
	subscribe(t, slow, canvasA) // and then never reads again

	big := map[string]string{"pad": strings.Repeat("x", 64<<10)}
	for i := 0; hub.Stats().DroppedSlow == 0; i++ {
		if i == 2000 {
			t.Fatal("slow client was never dropped")
		}
		hub.Publish(CanvasTopic(canvasA), big)
		if _, err := read(fast, 2*time.Second); err != nil { // paced by the fast reader
			t.Fatalf("fast client stopped receiving after %d messages: %v", i, err)
		}
	}

	hub.Publish(CanvasTopic(canvasA), PixelPlaced(canvasA, 7, 0, 0, 0))
	for {
		m, err := read(fast, 2*time.Second)
		if err != nil {
			t.Fatalf("fast client after the drop: %v", err)
		}
		if m.PixelId != nil && *m.PixelId == 7 {
			break
		}
	}
	if d := hub.Stats().DroppedSlow; d != 1 {
		t.Errorf("dropped %d clients, want 1", d)
	}
}

func TestCloseSaysGoingAway(t *testing.T) {
	hub, url := newServer(t)
	c := dial(t, url)
	subscribe(t, c, canvasA)

	go hub.Close()
	_, err := read(c, 5*time.Second)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Errorf("close status = %v (err %v), want going away", websocket.CloseStatus(err), err)
	}
}
