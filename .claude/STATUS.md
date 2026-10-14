# Status — where we are

**Updated 2026-10-14.** Update this file at the end of every session.

## Done and live

| Step | What | Task file |
| --- | --- | --- |
| 1 | Railway billing, usage limit | — |
| 2 | Go API with `/health`, Dockerfile, Railway deploy | 001 |
| 3 | Postgres, goose migrations, RLS, OpenAPI + sqlc, canvas endpoints | 002 |
| 4 | Vite + React frontend on Cloudflare Pages, CORS | 003 |
| 5 | Login, invites, spaces in the URL, rate limits, `cli bootstrap` | 004 |
| 6 | Placing pixels with a cooldown, live over WebSocket, zoom/pan/grid, optimistic draw | 005 |

- API: https://rail-canvas-production.up.railway.app (Railway service `api`, region Singapore)
- Site: https://rail-canvas.pages.dev (Cloudflare Pages, root `web`)
- Production space: `pixel-club`, owner `test1@example.com`. The password is `OWNER_PASSWORD` in local `.env`
- Plan of record: [Notion page](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9)

## Next

**Step 7 — CI: merged 2026-10-14** (task 006). CI runs on every pull request and push to `main`.
But `railway.api.json` was never read by Railway (Config as Code is deprecated), so the
pre-deploy migration step is **not live yet**.

**Step 7b — Railway as code** (task 007), in progress on branch `infra/railway-iac`:
`.railway/railway.ts` pulled from production, with start command, pre-deploy
`/app/cli migrate` and Wait for CI added. Local plan: 2 to change, 0 to destroy. Remaining:

1. Owner: Railway → Project Settings → Tokens → new token for `production` (`github-iac`); GitHub → Settings → Secrets and variables → Actions → `RAILWAY_TOKEN`
2. Pull request; read the plan comment together; merge; the apply run goes green
3. Check: deploy log shows `migrations: up to date`; `/health`; place a pixel
4. GitHub rule on `main` (pull request, checks `api`, `contract`, `web`, `docker`, no force push); practise one rollback
5. Notion: add Step 7b, tick 7 and 7b

The laptop has the Railway CLI (brew), logged in, linked to `rail-canvas` / `production`.
From now on: work on a branch, merge by pull request.

## Open items

- **Postgres public TCP proxy: deleted 2026-10-04, and stays deleted.** The laptop cannot reach the database; the live app reaches it over the private network, and migrations run in Railway's pre-deploy command. `MIGRATE_DATABASE_URL` and `DATABASE_URL` in local `.env` are stale; `make migrate-up`, `make test-db` and `make db-test-setup` no longer work from the laptop (CI runs the database tests). `rail_canvas_test` on the production server is no longer migrated: delete it once nothing needs it.
- **`DEV_SPACE_ID`** on the Railway `api` service: owner was asked to delete it after Step 5. Unused by the code either way; check and remove if still there.
- **Step 17 (load test):** raise the per-IP login limit in staging first (note in task 004). The user-flow draft is in Notion under Load test.

## Not in git (lives on the owner's laptop)

- `rail-canvas/.env`: `MIGRATE_DATABASE_URL` (postgres, public proxy), `APP_DB_PASSWORD`, `DATABASE_URL` (app role, public proxy, for local runs), `OWNER_PASSWORD`, plus leftover `DEV_SPACE_ID` (ignored)
- `rail-canvas/web/.env.local`: `VITE_API_URL=http://localhost:8080`
- Railway variables on `api`: `APP_DB_PASSWORD`, `DATABASE_URL` (references), `CORS_ORIGINS`

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
