package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func telegramID() string { return uuid.NewString()[:12] }

func TestBotLinkCodeWorksOnce(t *testing.T) {
	space, user, canvas, svc, _ := memberWithCanvas(t, 10)
	bot := NewBot(svc.pool, svc)
	ctx := context.Background()

	code, _, err := bot.MakeLinkCode(ctx, user, space, canvas)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 9 || code[4] != '-' {
		t.Errorf("code %q, want XXXX-XXXX", code)
	}
	// Typed lowercase without the dash, as people do.
	l, err := bot.Link(ctx, "telegram", telegramID(), code[:4]+code[5:])
	if err != nil || l.DisplayName != "Tester" || l.CanvasName != "pixels" {
		t.Fatalf("link: %+v, %v", l, err)
	}
	if _, err := bot.Link(ctx, "telegram", telegramID(), code); !errors.Is(err, ErrInvalidLinkCode) {
		t.Errorf("used twice: err = %v, want ErrInvalidLinkCode", err)
	}
	if _, err := bot.Link(ctx, "telegram", telegramID(), "AAAA-AAAA"); !errors.Is(err, ErrInvalidLinkCode) {
		t.Errorf("wrong code: err = %v, want ErrInvalidLinkCode", err)
	}

	expired, _, _ := bot.MakeLinkCode(ctx, user, space, canvas)
	if _, err := svc.pool.Exec(ctx, "UPDATE link_codes SET expires_at = now() - interval '1 second' WHERE code_hash = $1",
		hashSecret(normalizeCode(expired))); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Link(ctx, "telegram", telegramID(), expired); !errors.Is(err, ErrInvalidLinkCode) {
		t.Errorf("expired: err = %v, want ErrInvalidLinkCode", err)
	}

	other := newSpace(t, svc.pool)
	if _, _, err := bot.MakeLinkCode(ctx, user, other, canvas); !errors.Is(err, ErrNotFound) {
		t.Errorf("canvas of another space: err = %v, want ErrNotFound", err)
	}
}

func TestBotLinkAgainReplaces(t *testing.T) {
	space, user, canvas, svc, _ := memberWithCanvas(t, 10)
	bot := NewBot(svc.pool, svc)
	ctx := context.Background()
	second, err := NewCanvases(svc.pool).Create(ctx, space, user, "second", 10)
	if err != nil {
		t.Fatal(err)
	}
	link := func(tg string, user, canvas uuid.UUID) {
		t.Helper()
		code, _, err := bot.MakeLinkCode(ctx, user, space, canvas)
		if err == nil {
			_, err = bot.Link(ctx, "telegram", tg, code)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	linked := func(tg string) (user, canvas uuid.UUID) {
		t.Helper()
		if err := svc.pool.QueryRow(ctx, "SELECT user_id, canvas_id FROM bot_links WHERE platform = 'telegram' AND platform_user_id = $1",
			tg).Scan(&user, &canvas); err != nil {
			t.Fatal(err)
		}
		return user, canvas
	}

	tg := telegramID()
	link(tg, user, canvas)
	link(tg, user, second.ID)
	if _, c := linked(tg); c != second.ID {
		t.Errorf("after linking from the second canvas, linked to %v, want %v", c, second.ID)
	}

	// A second Telegram account may link to the same member.
	tg2 := telegramID()
	link(tg2, user, canvas)
	if u, _ := linked(tg2); u != user {
		t.Errorf("second account linked to %v, want %v", u, user)
	}

	// One Telegram account never points at two members: the newer link wins.
	other := newUser(t, svc.pool, "x")
	addMember(t, svc.pool, space, other, "member")
	link(tg, other, canvas)
	if u, _ := linked(tg); u != other {
		t.Errorf("relinked to %v, want %v", u, other)
	}
	var n int
	_ = svc.pool.QueryRow(ctx, "SELECT count(*) FROM bot_links WHERE platform_user_id = $1", tg).Scan(&n)
	if n != 1 {
		t.Errorf("%d links for one account, want 1", n)
	}
}

func TestBotPlace(t *testing.T) {
	space, user, canvas, svc, rec := memberWithCanvas(t, 60)
	bot := NewBot(svc.pool, svc)
	ctx := context.Background()
	tg := telegramID()

	if _, err := bot.Place(ctx, "telegram", tg, 1, 1, "red"); !errors.Is(err, ErrNotLinked) {
		t.Errorf("not linked: err = %v, want ErrNotLinked", err)
	}
	code, _, _ := bot.MakeLinkCode(ctx, user, space, canvas)
	if _, err := bot.Link(ctx, "telegram", tg, code); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Place(ctx, "telegram", tg, 1, 1, "mauve"); !errors.Is(err, ErrUnknownColor) {
		t.Errorf("unknown colour: err = %v, want ErrUnknownColor", err)
	}

	p, err := bot.Place(ctx, "telegram", tg, 10, 20, "Red")
	if err != nil {
		t.Fatal(err)
	}
	if p.Color != 5 || p.ColorName != "red" || rec.count() != 1 {
		t.Errorf("placed colour %d %q, %d announced; want 5 red, 1", p.Color, p.ColorName, rec.count())
	}
	if n := tenantCount(t, svc.pool, space, "SELECT count(*) FROM pixels WHERE id = $1 AND source = 'bot'", p.PixelID); n != 1 {
		t.Errorf("%d pixels with source bot, want 1", n)
	}

	// Web and bot share the member's cooldown.
	if _, err := bot.Place(ctx, "telegram", tg, 11, 20, "blue"); !errors.As(err, new(*CooldownError)) {
		t.Errorf("bot again: err = %v, want CooldownError", err)
	}
	if _, err := svc.Place(ctx, space, canvas, user, 12, 20, 1, "web"); !errors.As(err, new(*CooldownError)) {
		t.Errorf("web after bot: err = %v, want CooldownError", err)
	}

	// No longer a member: the link stays but places nothing.
	tenantExec(t, svc.pool, space, "DELETE FROM space_members WHERE space_id = $1 AND user_id = $2", space, user)
	if _, err := bot.Place(ctx, "telegram", tg, 13, 20, "blue"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed member: err = %v, want ErrNotFound", err)
	}
}

func TestBotPlaceSuspended(t *testing.T) {
	space, user, canvas, svc, _ := memberWithCanvas(t, 10)
	bot := NewBot(svc.pool, svc)
	ctx := context.Background()
	tg := telegramID()
	code, _, _ := bot.MakeLinkCode(ctx, user, space, canvas)
	if _, err := bot.Link(ctx, "telegram", tg, code); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Place(ctx, "telegram", tg, 1, 1, "red"); !errors.Is(err, ErrNotFound) {
		t.Errorf("suspended: err = %v, want ErrNotFound", err)
	}
	// A code made before the suspension cannot be redeemed after it.
	if _, err := svc.pool.Exec(ctx, "UPDATE users SET status = 'active' WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	late, _, _ := bot.MakeLinkCode(ctx, user, space, canvas)
	if _, err := svc.pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Link(ctx, "telegram", telegramID(), late); !errors.Is(err, ErrInvalidLinkCode) {
		t.Errorf("suspended, linking: err = %v, want ErrInvalidLinkCode", err)
	}
}

func TestPaletteColorNames(t *testing.T) {
	for id, p := range palettes {
		if len(p.Names) != len(p.Colors) {
			t.Fatalf("%s: %d names for %d colours", id, len(p.Names), len(p.Colors))
		}
		seen := map[string]bool{}
		for i, n := range p.Names {
			if seen[n] {
				t.Errorf("%s: name %q twice", id, n)
			}
			seen[n] = true
			if got, ok := p.Color(n); !ok || got != i {
				t.Errorf("%s: %q resolves to %d, %v; want %d", id, n, got, ok, i)
			}
		}
	}
	for id, p := range palettes {
		seen := map[string]bool{}
		for _, c := range p.Colors {
			if seen[c] {
				t.Errorf("%s: colour %s twice", id, c)
			}
			seen[c] = true
		}
	}
	if got, ok := palettes["place32"].Color("Dark Gray"); !ok || got != 3 {
		t.Errorf("place32 \"Dark Gray\": got %d, %v; want 3", got, ok)
	}
	p := palettes["classic16"]
	for word, want := range map[string]int{"Dark Blue": 13, "dark-blue": 13, "gray": 2, "5": 5, "0": 0} {
		if got, ok := p.Color(word); !ok || got != want {
			t.Errorf("%q: got %d, %v; want %d", word, got, ok, want)
		}
	}
	for _, word := range []string{"16", "-1", "mauve", ""} {
		if _, ok := p.Color(word); ok {
			t.Errorf("%q resolved, want refused", word)
		}
	}
}
