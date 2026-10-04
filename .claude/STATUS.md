# Status — where we are

**Updated 2026-10-04, end of session.** Update this file at the end of every session.

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

**Step 7 — CI.** Task 006 is not written yet. Start by writing it and getting approval.
Scope from Notion: GitHub Actions on every pull request (vet, test, contract check,
migrations against a throwaway Postgres, Docker build, frontend build), branch
protection on `main`, Railway waits for green checks, migrations run from CI,
practise one rollback. Then remove the Postgres public TCP proxy.

## Open items

- **Postgres public TCP proxy.** Added in Step 3 so the laptop could run migrations (`MIGRATE_DATABASE_URL`). Owner was advised to delete it for the break; if it is gone and is needed again, re-add it (Postgres → Settings → Networking → TCP Proxy, port 5432) and update `MIGRATE_DATABASE_URL`, since the port may change. Step 7 makes it unnecessary.
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
