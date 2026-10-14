# Status — where we are

**Updated 2026-10-15, end of session.** Update this file at the end of every session.

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
| 8 | Templates: PNG upload straight to a private bucket (`templates`, Singapore) with signed URLs, overlay on the board | 008 |

- API: https://rail-canvas-production.up.railway.app (Railway service `api`, region Singapore)
- Site: https://rail-canvas.pages.dev (Cloudflare Pages, root `web`)
- Production space: `pixel-club`, owner `test1@example.com`. The password is `OWNER_PASSWORD` in local `.env`
- Plan of record: [Notion page](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9)

## Next

**Step 9 — nightly `pg_dump` to Cloudflare R2** (task 009, not written yet). Start by writing it
and getting approval. From Notion: an `ops/backup` image `FROM postgres:18` (a `pg_dump` older
than the server refuses to dump it), run as a Railway cron service, connecting as `postgres`;
a dead-man's switch ping (healthchecks.io); one test restore into a temporary Postgres
(create the `app` role first, compare row counts, delete it); then `docs/runbook.md`.
Open question to check first: does the Backups tab exist on Hobby. The bucket has no backups
either (Railway offers none); decide whether templates are worth backing up.

How changes reach production now: branch → pull request → CI's four checks (required by the
`main` ruleset, squash merge only) → merge → Railway waits for CI → pre-deploy `cli migrate` →
deploy. A change to `.railway/` also gets a Railway plan comment and is applied on merge.

## Open items

- **Postgres public TCP proxy: deleted 2026-10-04, and stays deleted.** The laptop cannot reach the database; the live app reaches it over the private network, and migrations run in Railway's pre-deploy command. `MIGRATE_DATABASE_URL` and `DATABASE_URL` in local `.env` are stale; `make migrate-up`, `make test-db` and `make db-test-setup` no longer work from the laptop (CI runs the database tests). `rail_canvas_test` on the production server is no longer migrated: delete it once nothing needs it.
- **`DEV_SPACE_ID`** is gone from the `api` service (the pull from Railway on 2026-10-14 lists only `APP_DB_PASSWORD`, `CORS_ORIGINS`, `DATABASE_URL`, `MIGRATE_DATABASE_URL`).
- **Rollback on Railway restores the old deployment's settings too** (no pre-deploy step on a deployment from before 7b). Fine: a rollback must never need a migration.
- **Step 17 (load test):** raise the per-IP login limit in staging first (note in task 004). The user-flow draft is in Notion under Load test.

## Not in git (lives on the owner's laptop)

- `rail-canvas/.env`: `MIGRATE_DATABASE_URL` (postgres, public proxy), `APP_DB_PASSWORD`, `DATABASE_URL` (app role, public proxy, for local runs), `OWNER_PASSWORD`, plus leftover `DEV_SPACE_ID` (ignored)
- `rail-canvas/web/.env.local`: `VITE_API_URL=http://localhost:8080`
- Railway variable values on `api` (names are in `.railway/railway.ts`): `APP_DB_PASSWORD`, `DATABASE_URL` and `MIGRATE_DATABASE_URL` (references), `CORS_ORIGINS`
- GitHub secret `RAILWAY_TOKEN`: Railway project token `github-iac`, `production`
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
