# Status — where we are

**Updated 2026-10-18 (evening).** Update this file at the end of every session.

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
| 13 | Cron services: `snapshot` (hourly, queues the worker's pass; the API's timer is gone) and `cleanup` (20:00 UTC, healthchecks.io check `rail-canvas cleanup`). Out of dry run since PR #23 (2026-10-18); the dry run counted 2,702 old tickets. **First real run 2026-10-18 20:00 UTC, still to read** | 013 |
| 14 | Two API replicas; placements pass between them through Redis pub/sub (`realtime.Bus`), `resync` after a gap. Cross-copy test passed live (10 sockets split 5/5, one pixel, every socket got it once) | 014 |
| 15 | Telegram bot as a Railway Function (`bot`, Bun, `functions/webhook/index.ts`): `/link CODE` with a one-time code from the canvas page, `/place X Y COLOUR` through the API with the web's cooldown, `source = 'bot'`. Live test with fake updates passed 2026-10-18; no real Telegram bot connected yet (runbook) | 015 |
| 16 | `staging` environment before production: branch `staging` → staging (`Postgres-staging`, `Redis-staging`, `templates-staging`, seeded `staging-club`), release PR `staging` → `main` (merge commit; CI + `staging` status from `staging-tests.yml` required). `/health` reports the commit. Palette `place32` shipped through it. PR environment skipped | 016 |

- API: https://rail-canvas-production.up.railway.app (Railway service `api`, region Singapore)
- Site: https://rail-canvas.pages.dev (Cloudflare Pages, root `web`)
- Staging: API https://api-staging-9e8a.up.railway.app, site https://staging.rail-canvas.pages.dev (Pages preview, `VITE_API_URL` points at staging). Seeded owner `staging-owner@example.com`, password `SEED_OWNER_PASSWORD` in local `.env`
- Production space: `pixel-club`, owner `test1@example.com`. The password is `OWNER_PASSWORD` in local `.env`
- Plan of record: [Notion page](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9)

## Next

1. **Step 13:** read the first real cleanup (2026-10-18 20:00 UTC): about 2,702 tickets deleted,
   healthchecks.io green. Then tick Step 13
2. **Step 17** (task 017 to write): the k6 load test against staging (raise staging's login limits first)
3. **Optional:** connect a real Telegram bot (BotFather, `setWebhook`; `docs/runbook.md`)

**Release away from :50–:05 UTC, every time.** Each merge into `main` redeploys every production
service, the cron ones included; a cron service mid-deploy at the full hour skips that run (10:00 UTC on 2026-10-17
had no snapshot because PR #21 was merged at 09:56).

How changes reach production now (Step 16): local branch → merge into `staging` → push (only with
the owner's OK) → CI → staging deploys (its Railway settings applied on the push) → `staging-tests.yml`
sets the `staging` status → release PR `staging` → `main` (ruleset: CI's four checks + `staging`,
merge commit only) → owner merges → Railway waits for CI → pre-deploy → production deploys. A change
to `.railway/` gets production's plan on the release PR. After a release, merge `main` back into
`staging` (no content change) so the branches share history.

## Open items

- **Postgres public TCP proxy: deleted 2026-10-04, and stays deleted.** The laptop cannot reach the database; the live app reaches it over the private network, and migrations run in Railway's pre-deploy command. `MIGRATE_DATABASE_URL` and `DATABASE_URL` in local `.env` are stale; `make migrate-up`, `make test-db` and `make db-test-setup` no longer work from the laptop (CI runs the database tests). `rail_canvas_test` on the production server is no longer migrated: delete it once nothing needs it.
- **`DEV_SPACE_ID`** is gone from the `api` service (the pull from Railway on 2026-10-14 lists only `APP_DB_PASSWORD`, `CORS_ORIGINS`, `DATABASE_URL`, `MIGRATE_DATABASE_URL`).
- **Rollback on Railway restores the old deployment's settings too** (no pre-deploy step on a deployment from before 7b). Fine: a rollback must never need a migration.
- **Backups:** check healthchecks.io now and then (green = last night's backup arrived). Railway's own Backups tab is Pro only. The template images are not backed up (decision in task 009). R2 still holds the laptop test dump `postgres/2026/10/15/153956-rail_canvas` (fake data); it expires on its own, or delete it
- **Redis** holds copies only. `railway restart --service Redis` works but the CLI call does not return; use the dashboard. Redis keeps its data on its own volume across restarts (the template's)
- **`ws_tickets` keeps every used ticket** (2,702 rows on 2026-10-17). Since Step 11 new tickets go to Redis; the first real cleanup (2026-10-18 20:00 UTC) should empty it
- **Step 17 (load test):** raise the per-IP login limit in staging first (note in task 004). The user-flow draft is in Notion under Load test.

## Not in git (lives on the owner's laptop)

- `rail-canvas/.env`: `MIGRATE_DATABASE_URL` (postgres, public proxy), `APP_DB_PASSWORD`, `DATABASE_URL` (app role, public proxy, for local runs), `OWNER_PASSWORD`, `TELEGRAM_SECRET_TOKEN`, plus leftover `DEV_SPACE_ID` (ignored)
- `rail-canvas/web/.env.local`: `VITE_API_URL=http://localhost:8080`
- Railway variable values on `api` (names are in `.railway/railway.ts`): `APP_DB_PASSWORD`, `DATABASE_URL` and `MIGRATE_DATABASE_URL` (references), `CORS_ORIGINS`, `WEBHOOK_SECRET` (copy in Passwords, "rail-canvas webhook secret"; Step 15's Function needs it). `preserve()` keeps a value but never creates one: a new secret variable is added by hand in the dashboard
- `bot` (Railway Function): public domain `bot-production-2161.up.railway.app` (generated in the dashboard); `TELEGRAM_SECRET_TOKEN` (copy in Passwords, "rail-canvas telegram secret", and in local `.env`). Its `WEBHOOK_SECRET` is a reference to `api`'s
- GitHub secret `RAILWAY_TOKEN`: Railway project token `github-iac`, `production`
- GitHub secrets `RAILWAY_TOKEN_STAGING` (token `github-iac-staging`, `staging`) and `STAGING_OWNER_PASSWORD` (the seeded owner's), repository variable `STAGING_URL`
- Staging's `api` variables `APP_DB_PASSWORD` and `SEED_OWNER_PASSWORD`, made by Claude (fake data; not in Passwords). Lost: empty staging's database and set new ones
- Cloudflare Pages: Preview variable `VITE_API_URL` = staging's API
- `backup` variable values on Railway: `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `HC_PING_URL` (copies in the owner's Passwords app, "rail-canvas R2 backup" and "rail-canvas healthchecks")
- Cloudflare: R2 bucket `rail-canvas-backups` (30-day lifecycle rule) and its Account API token "rail-canvas backup"; a card is on file for R2, usage stays in the free tier
- healthchecks.io: free account, checks `rail-canvas backup` (cron `0 19 * * *` UTC) and `rail-canvas cleanup` (cron `0 20 * * *` UTC), grace 1 hour, email. The cleanup's ping URL is on the `cleanup` service and in Passwords
- GitHub ruleset `main` (JSON import, not kept in git; since 2026-10-18 merge commits only, and the `staging` status required beside the four CI checks)
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
- Work on a local branch; merge into `staging`; **ask before every push to `staging`**; releases are PRs the owner merges
- Commits: `type(scope): sentence`, **no `Co-Authored-By` trailer**
- Comments: one short line, only where the why is not obvious
- The owner is learning: explain in plain terms when asked, no jargon without a reason
