# Status — where we are

**Updated 2026-10-17 (late), end of session.** Update this file at the end of every session.

## Done and live

| Step | What | Task file |
| --- | --- | --- |
| 1 | Railway billing, usage limit | — |
| 2 | Go API with `/health`, Dockerfile, Railway deploy | 001 |
| 3 | Postgres, goose migrations, RLS, OpenAPI + sqlc, canvas endpoints | 002 |
| 4 | Vite + React frontend on Cloudflare Pages, CORS | 003 |
| 5 | Login, invites, spaces in the URL, rate limits, `cli bootstrap` | 004 |
| 6 | Placing pixels with a cooldown, live over WebSocket, zoom/pan/grid, optimistic draw | 005 |
| 7 | CI on GitHub Actions (api, contract, web, docker); `main` protected by a ruleset | 006 |
| 7b | Railway settings as code (`.railway/railway.ts`), migrations in the pre-deploy step, Wait for CI; one rollback practised | 007 |
| 8 | Templates: PNG upload straight to a private bucket (`templates`, Singapore) with signed URLs, overlay on the board. Checked again on production 2026-10-15 (a 32×32 test template on pixel-club's canvas at 10, 10) | 008 |
| 9 | Nightly `pg_dump` to Cloudflare R2 (cron service `backup`, 19:00 UTC), healthchecks.io dead-man's switch, one real restore with matching row counts, `docs/runbook.md` | 009 |
| 10 | Hourly canvas snapshots (a ticker inside the API, PNGs in the templates bucket, `GET …/snapshots`) and `POST /webhooks/…/pixels` with a shared secret (`WEBHOOK_SECRET`) | 010 |
| 11 | Redis (private, 8.2): board cache with compare-and-set and single-flight rebuild, cooldown check, shared rate limits, WebSocket tickets; Postgres and memory as fallbacks, a 5-second circuit breaker; live restart drill passed | 011 |
| 12 | Worker service (`/app/worker`, asynq on Redis): template thumbnails, the hourly snapshot pass, timelapse GIFs; at most 3 retries with exponential backoff. **Live; two checks left (see Next)** | 012 |

- API: https://rail-canvas-production.up.railway.app (Railway service `api`, region Singapore)
- Site: https://rail-canvas.pages.dev (Cloudflare Pages, root `web`)
- Production space: `pixel-club`, owner `test1@example.com`. The password is `OWNER_PASSWORD` in local `.env`
- Plan of record: [Notion page](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9)

## Next

**Finish Step 12 (worker), merged and deployed 2026-10-17 (PR #16).** Done live: `worker started`,
the `timelapses` migration, a timelapse of pixel-club's canvas built by the worker (2 frames,
0.37 s), the API's stats line shows the queue (all zero). Still to check, then close the step
(Notion tick, task 012 status, a docs PR):

1. The worker's log shows `job done type="snapshots:pass"` at a full hour (and the API's log no
   longer shows `msg=snapshots` passes, only `snapshots queued`)
2. The owner uploads a template (PNG, at most 256 × 256 and 1 MB) and its thumbnail appears;
   the worker's log shows `template:thumbnail`. The old checkerboard template has no thumbnail
   (uploaded before the worker): expected

Then **Step 13 — cron services** (task 013): the hourly snapshot timer moves out of the API into a
cron service that queues `snapshots:pass`, plus a nightly cleanup (expired sessions, used
tickets and codes, `ws_tickets`, pixel history past a retention window, old timelapses).

How changes reach production now: branch → pull request → CI's four checks (required by the
`main` ruleset, squash merge only) → merge → Railway waits for CI → pre-deploy `cli predeploy`
→ deploy. A change to `.railway/` also gets a Railway plan comment and is applied on merge.

## Open items

- **Postgres public TCP proxy: deleted 2026-10-04, and stays deleted.** The laptop cannot reach the database; the live app reaches it over the private network, and migrations run in Railway's pre-deploy command. `MIGRATE_DATABASE_URL` and `DATABASE_URL` in local `.env` are stale; `make migrate-up`, `make test-db` and `make db-test-setup` no longer work from the laptop (CI runs the database tests). `rail_canvas_test` on the production server is no longer migrated: delete it once nothing needs it.
- **`DEV_SPACE_ID`** is gone from the `api` service (the pull from Railway on 2026-10-14 lists only `APP_DB_PASSWORD`, `CORS_ORIGINS`, `DATABASE_URL`, `MIGRATE_DATABASE_URL`).
- **Rollback on Railway restores the old deployment's settings too** (no pre-deploy step on a deployment from before 7b). Fine: a rollback must never need a migration.
- **Backups:** check healthchecks.io now and then (green = last night's backup arrived). Railway's own Backups tab is Pro only. The template images are not backed up (decision in task 009). R2 still holds the laptop test dump `postgres/2026/10/15/153956-rail_canvas` (fake data); it expires on its own, or delete it
- **Redis** holds copies only. `railway restart --service Redis` works but the CLI call does not return; use the dashboard. Redis keeps its data on its own volume across restarts (the template's)
- **The snapshot ticker lives in the API process** (Step 10). It must move to a cron service (Step 13) before a second API replica (Step 14), or each replica snapshots
- **`ws_tickets` keeps every used ticket** (2,544 rows on 2026-10-15). Since Step 11 new tickets go to Redis and expire by themselves; the table is only the fallback. Step 13's cleanup empties it
- **Step 17 (load test):** raise the per-IP login limit in staging first (note in task 004). The user-flow draft is in Notion under Load test.

## Not in git (lives on the owner's laptop)

- `rail-canvas/.env`: `MIGRATE_DATABASE_URL` (postgres, public proxy), `APP_DB_PASSWORD`, `DATABASE_URL` (app role, public proxy, for local runs), `OWNER_PASSWORD`, plus leftover `DEV_SPACE_ID` (ignored)
- `rail-canvas/web/.env.local`: `VITE_API_URL=http://localhost:8080`
- Railway variable values on `api` (names are in `.railway/railway.ts`): `APP_DB_PASSWORD`, `DATABASE_URL` and `MIGRATE_DATABASE_URL` (references), `CORS_ORIGINS`, `WEBHOOK_SECRET` (copy in Passwords, "rail-canvas webhook secret"; Step 15's Function needs it). `preserve()` keeps a value but never creates one: a new secret variable is added by hand in the dashboard
- GitHub secret `RAILWAY_TOKEN`: Railway project token `github-iac`, `production`
- `backup` variable values on Railway: `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `HC_PING_URL` (copies in the owner's Passwords app, "rail-canvas R2 backup" and "rail-canvas healthchecks")
- Cloudflare: R2 bucket `rail-canvas-backups` (30-day lifecycle rule) and its Account API token "rail-canvas backup"; a card is on file for R2, usage stays in the free tier
- healthchecks.io: free account, check `rail-canvas backup` (cron `0 19 * * *` UTC, grace 1 hour, email)
- GitHub ruleset `main` (made from a JSON import, not kept in git)
- The laptop's Railway CLI login and link (`rail-canvas` / `production`)

## Test database

`rail_canvas_test` on the same Postgres server, migrated in step with production
(`make db-test-setup`). It holds a smoke-test space `smoke` with owner
`smoke-owner@example.com` / `smoke-test-pw` (test data only) and canvases
"Smoke canvas" (10 s) and "Live test" (3 s). To run the app locally against it:
point `DATABASE_URL` at `rail_canvas_test`, then `make dev` and `make web-dev`.

## How we work (owner's preferences)

- Plan first: a task file per step, approved before building
- Ask before anything touches a database or production settings
- Show locally, then push, then verify live, then tick Notion
- Commits: `type(scope): sentence`, **no `Co-Authored-By` trailer**
- Comments: one short line, only where the why is not obvious
- The owner is learning: explain in plain terms when asked, no jargon without a reason
