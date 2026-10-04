# Task 001 — A Go `/health` endpoint, live on Railway

**Status: code built 2026-10-04 and checked locally (`make check` green, binary
answers `/health`, shuts down cleanly on SIGTERM). Docker build not run locally —
OrbStack is off by choice, Railway builds it. Waiting on Step 1 (billing) and the
Railway setup below.**

Plan: [Rail Canvas, Step 2](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9).

## The goal

A live URL that answers `{"status":"ok"}`. Small on purpose — the win is the
whole path working once: repo, Dockerfile, Railway build, healthcheck, logs.
Everything after builds on this path.

## What was built

| Piece | File | Why it is shaped this way |
| --- | --- | --- |
| Entry point | `api/cmd/api/main.go` | `run()` returns errors so `main` has one exit path. `signal.NotifyContext` on SIGINT/SIGTERM, then `srv.Shutdown` with a timeout |
| Config | `api/internal/config/config.go` | Read once at boot, fails fast on a bad value, so a broken deploy fails its healthcheck instead of half-working |
| Health | `api/internal/handler/health.go` | No dependencies, ever. A database blip should not get a healthy API restarted |
| Response helpers | `api/internal/httpx/respond.go` | One error shape from the first endpoint, so nothing ever needs migrating to it |
| Middleware | `api/internal/middleware/` | Request id (client value ignored — forgeable), `slog` request line, panic recovery |
| Image | `Dockerfile` | Builds every `cmd/` binary into `/app/`, so the worker and CLI later reuse it with a different start command. distroless, non-root |
| Railway config | `railway.api.json` | Healthcheck, start command and restart policy in git instead of in the dashboard |

**No `WriteTimeout` on the server.** WebSockets (Step 6) are long-lived and
would be cut. Per-route timeouts go in the middleware chain instead.

**`/health` logs at debug.** Railway's healthcheck and the uptime checker would
otherwise be most of the log.

## Railway setup (owner)

After Step 1 (old balance paid, plan active, usage limit set):

1. Push the repo to GitHub as `ahmadfahrezi81/rail-canvas`
2. Railway: **New Project → Deploy from GitHub repo** → `rail-canvas`. Region: **Singapore**
3. Service settings → **Config-as-code file path**: `railway.api.json`
4. Service settings → **Networking → Generate domain**
5. Watch the build log, then the deploy log: expect a `listening` JSON line
6. Open `https://<domain>/health` → `{"status":"ok","env":"production"}`
7. Point a free uptime checker (UptimeRobot, Better Stack or similar) at `/health`
8. Write the date next to Step 2 in Notion

**Check while there (open questions from Notion):** is Singapore offered for
every service type we will use (Postgres is already there; buckets and Functions
later)?

## How we know it works

- `make check` — gofmt, vet, tests (request id ignores a client value, recover
  returns 500 and re-panics `ErrAbortHandler`, health returns the right body)
- Locally: `make dev`, `curl localhost:8080/health`, Ctrl-C prints
  `shutting down` then `stopped cleanly`
- On Railway: the deploy goes green only if `/health` answers, and a redeploy
  shows `shutting down` / `stopped cleanly` in the old deploy's log
