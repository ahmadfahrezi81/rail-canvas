package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignupWithInvite(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	space, owner := newSpace(t, pool), newUser(t, pool, "x")
	addMember(t, pool, space, owner, "owner")
	auth := NewAuth(pool)

	code, expires, err := auth.CreateInvite(ctx, owner, space)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 26 || time.Until(expires) < 6*24*time.Hour {
		t.Errorf("code %q expires %v", code, expires)
	}

	email := "new-" + uuid.NewString()[:8] + "@Example.com"
	token, user, err := auth.Signup(ctx, SignupParams{Code: code, Email: email, DisplayName: "New", Password: "long enough pw"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })

	if user.Email != NormalizeEmail(email) {
		t.Errorf("email stored as %q, want lowercase", user.Email)
	}
	if got, err := auth.Authenticate(ctx, token); err != nil || got.ID != user.ID {
		t.Errorf("token: got %v, %v", got, err)
	}
	if role, err := NewSpaces(pool).Role(ctx, user.ID, space); err != nil || role != "member" {
		t.Errorf("role: got %q, %v", role, err)
	}
	if _, _, err := auth.Login(ctx, email, "long enough pw"); err != nil {
		t.Errorf("login after signup: %v", err)
	}
}

func TestInviteRules(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	space, owner, member, stranger := newSpace(t, pool), newUser(t, pool, "x"), newUser(t, pool, "x"), newUser(t, pool, "x")
	addMember(t, pool, space, owner, "owner")
	addMember(t, pool, space, member, "member")
	auth := NewAuth(pool)

	if _, _, err := auth.CreateInvite(ctx, member, space); !errors.Is(err, ErrForbidden) {
		t.Errorf("member invites: err = %v, want ErrForbidden", err)
	}
	if _, _, err := auth.CreateInvite(ctx, stranger, space); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger invites: err = %v, want ErrNotFound", err)
	}

	code, _, err := auth.CreateInvite(ctx, owner, space)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Join(ctx, stranger, code); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if _, err := auth.Join(ctx, member, code); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("reuse: err = %v, want ErrInvalidInvite", err)
	}
	if _, err := auth.Join(ctx, member, "AAAAAAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("made-up code: err = %v, want ErrInvalidInvite", err)
	}

	expired := newInviteCode()
	if _, err := pool.Exec(ctx, "INSERT INTO invites (space_id, code_hash, created_by, expires_at) VALUES ($1, $2, $3, now() - interval '1 second')",
		space, hashSecret(expired), owner); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Join(ctx, member, expired); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("expired: err = %v, want ErrInvalidInvite", err)
	}
}

func TestInviteRace(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	space, owner := newSpace(t, pool), newUser(t, pool, "x")
	addMember(t, pool, space, owner, "owner")
	auth := NewAuth(pool)
	code, _, err := auth.CreateInvite(ctx, owner, space)
	if err != nil {
		t.Fatal(err)
	}

	const racers = 2
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			email := "race-" + uuid.NewString()[:8] + "@example.com"
			_, user, err := auth.Signup(ctx, SignupParams{Code: code, Email: email, DisplayName: "R", Password: "long enough pw"})
			errs[i] = err
			if err == nil {
				t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })
			}
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, ErrInvalidInvite):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d signups used one code, want exactly 1", wins)
	}
}

func TestLoginFailuresLookAlike(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := newUser(t, pool, "right password")
	auth := NewAuth(pool)

	_, _, wrong := auth.Login(ctx, emailOf(t, pool, user), "wrong password")
	_, _, unknown := auth.Login(ctx, "nobody-"+uuid.NewString()[:8]+"@example.com", "whatever")
	if !errors.Is(wrong, ErrInvalidCredentials) || !errors.Is(unknown, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v; unknown email: %v; want both ErrInvalidCredentials", wrong, unknown)
	}
	if _, _, err := auth.Login(ctx, emailOf(t, pool, user), "right password"); err != nil {
		t.Errorf("right password: %v", err)
	}
}

func TestSessionsCanBeRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := newUser(t, pool, "right password")
	auth := NewAuth(pool)
	email := emailOf(t, pool, user)

	token, _, err := auth.Login(ctx, email, "right password")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("after logout: err = %v, want ErrUnauthenticated", err)
	}

	token, _, err = auth.Login(ctx, email, "right password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET status = 'suspended' WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("suspended: err = %v, want ErrUnauthenticated", err)
	}
	if _, _, err := auth.Login(ctx, email, "right password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("suspended login: err = %v, want ErrInvalidCredentials", err)
	}
}

func TestBootstrapRefusesOnceUsersExist(t *testing.T) {
	pool := testPool(t)
	newUser(t, pool, "x")
	_, err := NewAuth(pool).Bootstrap(context.Background(), BootstrapParams{
		SpaceName: "Nope", SpaceSlug: "nope-" + uuid.NewString()[:8], Email: "n@example.com", DisplayName: "N", Password: "long enough pw",
	})
	if !errors.Is(err, ErrAlreadyBootstrap) {
		t.Errorf("err = %v, want ErrAlreadyBootstrap", err)
	}
}
