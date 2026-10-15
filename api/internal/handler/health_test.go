package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/apigen"
)

func TestHealth(t *testing.T) {
	res, err := NewServer("test", nil, nil, nil, nil, nil, nil, "").GetHealth(context.Background(), apigen.GetHealthRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.(apigen.GetHealth200JSONResponse)
	if !ok || got.Status != "ok" || got.Env != "test" {
		t.Errorf("got %#v, want 200 with status ok, env test", res)
	}
}

func TestRoutesNeedAUser(t *testing.T) {
	s := NewServer("test", nil, nil, nil, nil, nil, nil, "")
	ctx := context.Background()

	if res, _ := s.ListCanvases(ctx, apigen.ListCanvasesRequestObject{}); !is[apigen.ListCanvases401JSONResponse](res) {
		t.Errorf("list: got %T, want 401", res)
	}
	if res, _ := s.CreateCanvas(ctx, apigen.CreateCanvasRequestObject{Body: &apigen.CreateCanvasRequest{Name: "x"}}); !is[apigen.CreateCanvas401JSONResponse](res) {
		t.Errorf("create: got %T, want 401", res)
	}
	if res, _ := s.GetCanvasBoard(ctx, apigen.GetCanvasBoardRequestObject{}); !is[apigen.GetCanvasBoard401JSONResponse](res) {
		t.Errorf("board: got %T, want 401", res)
	}
	if res, _ := s.GetMe(ctx, apigen.GetMeRequestObject{}); !is[apigen.GetMe401JSONResponse](res) {
		t.Errorf("me: got %T, want 401", res)
	}
	if res, _ := s.CreateInvite(ctx, apigen.CreateInviteRequestObject{}); !is[apigen.CreateInvite401JSONResponse](res) {
		t.Errorf("invite: got %T, want 401", res)
	}
	if res, _ := s.Logout(ctx, apigen.LogoutRequestObject{}); !is[apigen.Logout401JSONResponse](res) {
		t.Errorf("logout: got %T, want 401", res)
	}
	if res, _ := s.PlacePixel(ctx, apigen.PlacePixelRequestObject{}); !is[apigen.PlacePixel401JSONResponse](res) {
		t.Errorf("place: got %T, want 401", res)
	}
	if res, _ := s.CreateWSTicket(ctx, apigen.CreateWSTicketRequestObject{}); !is[apigen.CreateWSTicket401JSONResponse](res) {
		t.Errorf("ws ticket: got %T, want 401", res)
	}
}

func is[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

func TestWebhookSecret(t *testing.T) {
	ctx := context.Background()
	secret := strings.Repeat("s", 63) + "t"
	s := NewServer("test", nil, nil, nil, nil, nil, nil, secret)
	for name, got := range map[string]*string{
		"missing":          nil,
		"wrong":            ptr("nope"),
		"last char wrong":  ptr(strings.Repeat("s", 64)),
		"secret plus more": ptr(secret + "x"),
	} {
		req := apigen.WebhookPlacePixelRequestObject{Params: apigen.WebhookPlacePixelParams{XWebhookSecret: got}, Body: &apigen.WebhookPlacePixelRequest{}}
		if res, _ := s.WebhookPlacePixel(ctx, req); !is[apigen.WebhookPlacePixel401JSONResponse](res) {
			t.Errorf("%s: got %T, want 401", name, res)
		}
	}

	off := NewServer("test", nil, nil, nil, nil, nil, nil, "")
	req := apigen.WebhookPlacePixelRequestObject{Params: apigen.WebhookPlacePixelParams{XWebhookSecret: ptr(secret)}, Body: &apigen.WebhookPlacePixelRequest{}}
	if res, _ := off.WebhookPlacePixel(ctx, req); !is[apigen.WebhookPlacePixel503JSONResponse](res) {
		t.Errorf("no secret configured: got %T, want 503", res)
	}
}

func ptr[T any](v T) *T { return &v }
