# Status — where we are

**Updated 2026-10-17 (evening).** Update this file at the end of every session.

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
| 12 | Worker service (`/app/worker`, asynq on Redis): template thumbnails, the hourly snapshot pass, timelapse GIFs; at most 3 retries with exponential backoff. The live thumbnail check was skipped by the owner (tested locally) | 012 |
| 13 | Cron services: `snapshot` (hourly, queues the worker's pass; the API's timer is gone) and `cleanup` (20:00 UTC, healthchecks.io check `rail-canvas cleanup`). **Cleanup still a dry run** (first count: 2,702 old tickets) | 013 |
| 14 | Two API replicas; placements pass between them through Redis pub/sub (`realtime.Bus`), `resync` after a gap. **Live; the cross-copy test with the owner is left** | 014 |

- API: https://rail-canvas-production.up.railway.app (Railway service `api`, region Singapore)
- Site: https://rail-canvas.pages.dev (Cloudflare Pages, root `web`)
- Production space: `pixel-club`, owner `test1@example.com`. The password is `OWNER_PASSWORD` in local `.env`
- Plan of record: [Notion page](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9)

## Next

1. **Step 14 live test** (2 minutes, with the owner): open several listening sockets as the owner
   across both copies, the owner places one pixel, every socket gets it once; stats show
   `bus_published` on one copy and `bus_received` on the other. Then tick Step 14 in Notion
2. **Step 13:** after tonight's dry run (20:00 UTC), read its counts; then a one-line pull request
   drops `-dry-run` from `cleanup`'s start command. Then tick Step 13
3. **Step 15** (task 015 to write): the Railway Function for a Discord or Telegram `/place`, with
   bot account linking (`bot_links`, `link_codes`)

**Merge away from :50–:05 UTC, every time.** Each merge redeploys every service, the cron ones
included; a cron service mid-deploy at the full hour skips that run (10:00 UTC on 2026-10-17
had no snapshot because PR #21 was merged at 09:56).

How changes reach production now: branch → pull request → CI's four checks (required by the
`main` ruleset, squash merge only) → merge → Railway waits for CI → pre-deploy `cli predeploy`
→ deploy. A change to `.railway/` also gets a Railway plan comment and is applied on merge.

## Open items

- **Postgres public TCP proxy: deleted 2026-10-04, and stays deleted.** The laptop cannot reach the database; the live app reaches it over the private network, and migrations run in Railway's pre-deploy command. `MIGRATE_DATABASE_URL` and `DATABASE_URL` in local `.env` are stale; `make migrate-up`, `make test-db` and `make db-test-setup` no longer work from the laptop (CI runs the database tests). `rail_canvas_test` on the production server is no longer migrated: delete it once nothing needs it.
- **`DEV_SPACE_ID`** is gone from the `api` service (the pull from Railway on 2026-10-14 lists only `APP_DB_PASSWORD`, `CORS_ORIGINS`, `DATABASE_URL`, `MIGRATE_DATABASE_URL`).
- **Rollback on Railway restores the old deployment's settings too** (no pre-deploy step on a deployment from before 7b). Fine: a rollback must never need a migration.
- **Backups:** check healthchecks.io now and then (green = last night's backup arrived). Railway's own Backups tab is Pro only. The template images are not backed up (decision in task 009). R2 still holds the laptop test dump `postgres/2026/10/15/153956-rail_canvas` (fake data); it expires on its own, or delete it
- **Redis** holds copies only. `railway restart --service Redis` works but the CLI call does not return; use the dashboard. Redis keeps its data on its own volume across restarts (the template's)
- **`ws_tickets` keeps every used ticket** (2,702 rows on 2026-10-17). Since Step 11 new tickets go to Redis; the cleanup empties the table once it is out of dry run
- **Step 17 (load test):** raise the per-IP login limit in staging first (note in task 004). The user-flow draft is in Notion under Load test.

## Not in git (lives on the owner's laptop)

- `rail-canvas/.env`: `MIGRATE_DATABASE_URL` (postgres, public proxy), `APP_DB_PASSWORD`, `DATABASE_URL` (app role, public proxy, for local runs), `OWNER_PASSWORD`, plus leftover `DEV_SPACE_ID` (ignored)
- `rail-canvas/web/.env.local`: `VITE_API_URL=http://localhost:8080`
- Railway variable values on `api` (names are in `.railway/railway.ts`): `APP_DB_PASSWORD`, `DATABASE_URL` and `MIGRATE_DATABASE_URL` (references), `CORS_ORIGINS`, `WEBHOOK_SECRET` (copy in Passwords, "rail-canvas webhook secret"; Step 15's Function needs it). `preserve()` keeps a value but never creates one: a new secret variable is added by hand in the dashboard
- GitHub secret `RAILWAY_TOKEN`: Railway project token `github-iac`, `production`
- `backup` variable values on Railway: `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `HC_PING_URL` (copies in the owner's Passwords app, "rail-canvas R2 backup" and "rail-canvas healthchecks")
- Cloudflare: R2 bucket `rail-canvas-backups` (30-day lifecycle rule) and its Account API token "rail-canvas backup"; a card is on file for R2, usage stays in the free tier
- healthchecks.io: free account, checks `rail-canvas backup` (cron `0 19 * * *` UTC) and `rail-canvas cleanup` (cron `0 20 * * *` UTC), grace 1 hour, email. The cleanup's ping URL is on the `cleanup` service and in Passwords
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
