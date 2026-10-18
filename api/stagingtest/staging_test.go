//go:build staging

// Package stagingtest checks a deployed API end to end, over the real network: the pieces working
// together, not the logic (the unit and database tests cover that). It writes only to the seeded
// staging space; never point it at production.
//
//	STAGING_URL=https://… STAGING_OWNER_PASSWORD=… go test -tags staging ./stagingtest/
package stagingtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

type client struct {
	t     *testing.T
	base  string
	token string
}

// do sends a JSON request and decodes the reply into out (nil to skip); it returns the status.
func (c *client) do(method, path string, body, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode < 300 {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return res.StatusCode
}

func TestStaging(t *testing.T) {
	base, password := strings.TrimRight(os.Getenv("STAGING_URL"), "/"), os.Getenv("STAGING_OWNER_PASSWORD")
	if base == "" || password == "" {
		t.Fatal("STAGING_URL and STAGING_OWNER_PASSWORD are required")
	}
	if strings.Contains(base, "rail-canvas-production") {
		t.Fatal("refusing to run against production")
	}
	c := &client{t: t, base: base}

	var health apigen.Health
	if c.do("GET", "/health", nil, &health) != 200 || health.Status != "ok" {
		t.Fatalf("/health: %+v", health)
	}
	if want := os.Getenv("WANT_COMMIT"); want != "" && health.Commit != want {
		t.Fatalf("/health runs commit %q, want %q", health.Commit, want)
	}

	var auth apigen.AuthResponse
	if s := c.do("POST", "/auth/login", apigen.LoginRequest{Email: service.SeedOwnerEmail, Password: password}, &auth); s != 200 {
		t.Fatalf("login as the seeded owner: %d", s)
	}
	c.token = auth.Token

	var me apigen.Me
	c.do("GET", "/me", nil, &me)
	var space string
	for _, s := range me.Spaces {
		if s.Slug == service.SeedSpaceSlug {
			space = s.Id.String()
		}
	}
	if space == "" {
		t.Fatalf("the owner is not in %s", service.SeedSpaceSlug)
	}

	// A canvas per run, named for the commit, so runs can be told apart.
	name := "staging test " + time.Now().UTC().Format("2006-01-02 15:04:05")
	if commit := health.Commit; len(commit) >= 7 {
		name += " " + commit[:7]
	}
	one := 1
	var canvas apigen.Canvas
	if s := c.do("POST", "/spaces/"+space+"/canvases", apigen.CreateCanvasRequest{Name: name, CooldownSeconds: &one}, &canvas); s != 201 {
		t.Fatalf("create canvas: %d", s)
	}
	canvasPath := "/spaces/" + space + "/canvases/" + canvas.Id.String()

	t.Run("a placed pixel reaches a WebSocket and the board", func(t *testing.T) {
		var ticket apigen.WSTicket
		if s := c.do("POST", "/ws/ticket", nil, &ticket); s != 201 && s != 200 {
			t.Fatalf("ticket: %d", s)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+"/ws?ticket="+ticket.Ticket, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer ws.CloseNow()
		if err := wsjson.Write(ctx, ws, map[string]string{"type": "subscribe", "spaceId": space, "canvasId": canvas.Id.String()}); err != nil {
			t.Fatal(err)
		}
		waitFor(ctx, t, ws, "subscribed")

		var placed apigen.PlacedPixel
		if s := c.do("POST", canvasPath+"/pixels", apigen.PlacePixelRequest{X: 3, Y: 4, Color: 5}, &placed); s != 201 {
			t.Fatalf("place: %d", s)
		}
		msg := waitFor(ctx, t, ws, "pixel:placed")
		if msg.PixelId == nil || *msg.PixelId != placed.PixelId {
			t.Errorf("socket got %+v, want pixel %d", msg, placed.PixelId)
		}

		req, _ := http.NewRequest("GET", base+canvasPath+"/board", nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		board, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if len(board) != 256*256 || board[4*256+3] != 5 {
			t.Errorf("board: %d bytes, cell (3,4) = %v; want 65536 bytes and 5", len(board), board[4*256+3:4*256+4])
		}
	})

	t.Run("a second pixel at once is refused", func(t *testing.T) {
		time.Sleep(1100 * time.Millisecond) // the canvas's 1 s cooldown, from the subtest above
		if s := c.do("POST", canvasPath+"/pixels", apigen.PlacePixelRequest{X: 5, Y: 5, Color: 1}, nil); s != 201 {
			t.Fatalf("first: %d", s)
		}
		if s := c.do("POST", canvasPath+"/pixels", apigen.PlacePixelRequest{X: 6, Y: 5, Color: 1}, nil); s != 429 {
			t.Errorf("second at once: %d, want 429", s)
		}
	})

	t.Run("a bot link code is made", func(t *testing.T) {
		var code apigen.BotLinkCode
		if s := c.do("POST", canvasPath+"/bot-link-code", nil, &code); s != 201 || len(code.Code) != 9 {
			t.Errorf("link code: %d %q", s, code.Code)
		}
	})

	t.Run("a 32-colour canvas takes colour 31", func(t *testing.T) {
		place32 := apigen.Place32
		var big apigen.Canvas
		if s := c.do("POST", "/spaces/"+space+"/canvases", apigen.CreateCanvasRequest{Name: name + " place32", CooldownSeconds: &one, Palette: &place32}, &big); s != 201 || len(big.Palette.Colors) != 32 {
			t.Fatalf("create: %d, %d colours", s, len(big.Palette.Colors))
		}
		time.Sleep(1100 * time.Millisecond) // the space's turn, shared with the canvas above
		if s := c.do("POST", "/spaces/"+space+"/canvases/"+big.Id.String()+"/pixels", apigen.PlacePixelRequest{X: 1, Y: 1, Color: 31}, nil); s != 201 {
			t.Errorf("colour 31: %d, want 201", s)
		}
	})

	t.Run("a template goes to the bucket and back", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.Black)
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		var up apigen.TemplateUpload
		switch s := c.do("POST", canvasPath+"/templates", apigen.CreateTemplateRequest{Bytes: buf.Len(), ContentType: "image/png", X: 0, Y: 0}, &up); {
		case s == 503 && health.Env == "development":
			t.Skip("no bucket on the laptop")
		case s != 201:
			t.Fatalf("create template: %d", s)
		}
		req, _ := http.NewRequest("PUT", up.UploadUrl, bytes.NewReader(buf.Bytes()))
		for k, v := range up.UploadHeaders {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("upload to the bucket: %d", res.StatusCode)
		}
		var tpl apigen.Template
		if s := c.do("POST", canvasPath+"/templates/"+up.TemplateId.String()+"/complete", nil, &tpl); s != 200 {
			t.Fatalf("complete: %d", s)
		}
		res, err = http.Get(tpl.Url)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !bytes.Equal(got, buf.Bytes()) {
			t.Errorf("signed download: %d bytes (status %d), want the %d uploaded", len(got), res.StatusCode, buf.Len())
		}
	})
}

func waitFor(ctx context.Context, t *testing.T, ws *websocket.Conn, typ string) apigen.WSServerMessage {
	t.Helper()
	for {
		var m apigen.WSServerMessage
		if err := wsjson.Read(ctx, ws, &m); err != nil {
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		if string(m.Type) == typ {
			return m
		}
		if m.Type == "error" {
			t.Fatalf("waiting for %s: server error %s", typ, fmt.Sprint(m))
		}
	}
}
