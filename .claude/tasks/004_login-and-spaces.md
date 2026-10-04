# Task 004 — Login, invites and real spaces

**Status: built 2026-10-04 and pushed. Migrations applied to Railway and the test database; all 9 database tests green (the invite race 5 of 5 runs); a 17-check curl smoke test passed; the owner tried the frontend locally. Production bootstrapped (space `pixel-club`, owner login verified) and the dev space deleted.**

## What changed from the plan

- **The real-IP probe could not be read-only**: the live API had no endpoint that echoes headers. `RealIP` reads `X-Real-IP` only (Railway's edge sets it), and the check moves to after the deploy: six logins with different forged IPs must still hit 429.
- **A second, looser per-IP login limit** (20 at once, one per 3 s). Keying only on email + IP let one IP try many accounts.

Plan: [Rail Canvas, Step 5](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Security and tenancy" section.

## The goal

Every request acts as a logged-in user, inside a space they belong to. The
`DEV_SPACE_ID` stand-in is deleted. Nobody can sign up without an invite. This
closes the gap left open since Step 3: today anyone with the API URL can list
and create canvases.

## Decisions for this step

1. **The space is in the URL.** `/spaces/{spaceId}/canvases`,
   `/spaces/{spaceId}/canvases/{canvasId}/board`. A user can belong to several
   spaces, so each request must say which one; the path makes it explicit, like
   tea-pos's `/:tenantSlug/` routes. This replaces the Step 3 routes (no other
   client uses them).
2. **Membership is checked on every space route, live.** Not a member → 404,
   not 403, so a stranger cannot tell whether a space exists.
3. **Session token: 32 random bytes**, sent as `Authorization: Bearer …`. The
   database stores only its SHA-256 (a token is too random to guess, so a fast
   hash is enough; bcrypt is for passwords). Lifetime 30 days. Logout revokes it.
4. **Every request reads the session live**, joined with the user's status.
   Deliberate, same as tea-pos's live `users` read: a cached session would
   keep a suspended account working until it expired.
5. **Passwords: bcrypt cost 12.** Unknown email still runs a bcrypt compare
   against a dummy hash, and both failures return the same message, so login
   timing and errors do not reveal which emails exist.
6. **Invite codes: 128 random bits**, shown once as 26 base32 characters, stored
   as SHA-256, single-use, 7-day expiry. Redeeming is one atomic
   `UPDATE … WHERE used_at IS NULL AND expires_at > now() RETURNING space_id`,
   so two people racing for one code cannot both win.
7. **Rate limits in memory** (`golang.org/x/time/rate`): login keyed on email +
   IP, signup and join keyed on IP. Redis takes over in Step 11.
8. **Real client IP.** The limiter must use an IP the client cannot forge.
   First thing in the build: send a request with a fake `X-Forwarded-For` to
   the live API and see what Railway's edge passes through, then trust only
   that header (or only its last hop). Not chi's `RealIP`, which believes any
   header it finds.
9. **`cli bootstrap`** creates the first space and its owner. The password is
   read from the terminal, never a flag (flags end up in shell history).
10. **The dev space is deleted after bootstrap.** It only holds test canvases
    ("First canvas", "test"). A seed script removes it; its canvases cascade.
11. **Frontend keeps the token in `localStorage`.** Cookies would not work
    across `pages.dev` and `railway.app` (the reason bearer tokens were chosen).
    The trade-off: script injected into the page could read it. Acceptable here:
    React escapes output and the page loads no third-party scripts.

## Migrations (all additive, safe to run before the deploy)

| # | Contents | RLS |
| --- | --- | --- |
| 1 | `users (id, email unique and lowercase, password_hash, display_name, status active/suspended, created_at)` | No |
| 2 | `space_members (space_id, user_id, role owner/member, last_placed_at, joined_at)`, primary key on the pair | No |
| 3 | `sessions (id, user_id, token_hash unique, created_at, expires_at, revoked_at)` | No |
| 4 | `invites (id, space_id, code_hash unique, created_by, expires_at, used_by, used_at)` | No |
| 5 | `canvases.created_by` (nullable, references `users`) | already on |

Auth tables have no RLS: they are read before a space is known. Every query
on them filters in Go, as the plan says.

## Endpoints

| Method and path | Who | What |
| --- | --- | --- |
| `POST /auth/login` | anyone, rate limited | email + password → token |
| `POST /auth/signup` | anyone with a code, rate limited | code + email + name + password → new user, member of the code's space, token |
| `POST /auth/logout` | logged in | revokes this token |
| `GET /me` | logged in | user + their spaces (id, name, role) |
| `POST /spaces/join` | logged in, rate limited | code → member of another space |
| `POST /spaces/{spaceId}/invites` | owner | new code, shown once |
| `GET`, `POST /spaces/{spaceId}/canvases` | member | as Step 3 |
| `GET /spaces/{spaceId}/canvases/{canvasId}/board` | member | as Step 3 |

## Code

- `service/auth.go` — login, signup, logout, session lookup, invites
- `service/spaces.go` — memberships, `Me`
- `middleware/auth.go` — reads the bearer token, puts the user in the context
  (no token → no user; handlers return 401 where one is required)
- `middleware/realip.go`, `middleware/ratelimit.go`
- `handler` — space routes call one helper that checks membership and returns
  the space, or the 401/404 response
- `cmd/cli` — `bootstrap` subcommand; same image, so it also runs on Railway later
- Deleted: `middleware/space.go` (`DevSpace`), `DEV_SPACE_ID` in config,
  `db/seed/dev_space.sql`

**Frontend:** login form, signup-with-invite form, a space switcher when the
user has more than one, an "Invite" button for owners that shows the code once.
The api client attaches the token through an `openapi-fetch` middleware; a 401
clears it and shows the login form.

## Tests

Database tests (as `app`, against `rail_canvas_test`):

- signup with a code makes a member and a working token
- a used code, an expired code and a made-up code all fail
- **two signups racing for one code: exactly one succeeds**
- wrong password and unknown email fail with the same error
- a revoked token and a suspended user's token are both rejected
- a non-member gets not-found on another space's canvases
- only an owner can create invites

Unit tests: rate limiter (sixth attempt in a minute refused), real IP (a
forged header is ignored), handlers return 401 without a user.

## Order of work

1. Probe the live API for the real-IP header — **read-only, no changes**
2. Migrations, services, middleware, handlers, CLI, tests — **no database touched**
3. **Ask the owner**, then: migrations on Railway and the test database, `make test-db`
4. Frontend: login, signup, switcher, invite button; run locally and show the owner
5. **With the owner at the keyboard:** `cli bootstrap` (they type their own
   password), then delete the dev space
6. Owner removes `DEV_SPACE_ID` from Railway; push; check the live site:
   logged out sees the login form, logged in sees their space

## Out of scope

Password reset, email verification, changing a password, removing members,
Google OAuth. Each is a small later task if wanted.

## Note for Step 17 (load test)

k6 logs in each seeded player with `POST /auth/login` and sends the bearer
token, so it exercises login too. But all k6 traffic shares one IP, and the
per-IP login limit (20 at once, then one per 3 s) would stretch 200 logins
over about 9 minutes. Before the load test, make the limiter settings
environment variables and raise them in staging only.

## Live check (2026-10-04, after deploy)

Owner login works with CORS for the Pages origin; `/me` shows Pixel Club as
owner; the old `/canvases` route is gone (404); a space route without a token
is 401, another space's id is 404; a made-up invite code is refused. **Real IP
confirmed:** seven logins, each forging a different `X-Real-IP` and
`X-Forwarded-For`, still hit 429 on the sixth, so Railway's edge overwrites
`X-Real-IP` and the limiter cannot be dodged.
