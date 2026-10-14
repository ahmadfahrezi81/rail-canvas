# Task 007 — Railway settings live in git (Infrastructure as Code)

**Status: approved 2026-10-14. Built on branch `infra/railway-iac`; local plan shows exactly the intended changes. Waiting for the token (clicks 2 and 3), then the pull request.**

## What changed from the plan

- **The pull also found Wait for CI**: it is `checkSuites` on the GitHub source, so it moves into the file (`true`) instead of being a dashboard click.
- **Variables are managed by the file** (`--variables=managed`, which the CLI requires choosing): all four (`APP_DB_PASSWORD`, `CORS_ORIGINS`, `DATABASE_URL`, `MIGRATE_DATABASE_URL`) are declared as `preserve()`, so the values stay on Railway.
- **The first plan was clean** ("already up to date"). The plan with our edits: 0 to add, 2 to change, 0 to destroy (`checkSuites` null → true; `preDeployCommand` null → `/app/cli migrate`; `startCommand` null → `/app/api`).
- **The pull lists the builder as Railpack**, yet Railway builds the root `Dockerfile` (it does when one exists). Left as pulled; no change.
- **Watch patterns and the pre-deploy timeout do not appear in the pull** and stay dashboard settings (both unset today).

A step the Notion plan did not have, found while finishing Step 7. Call it
Step 7b; the bucket (Step 8) becomes task 008.

Docs: [Infrastructure as Code](https://docs.railway.com/infrastructure-as-code),
[reference](https://docs.railway.com/infrastructure-as-code/reference),
[railwayapp/config action](https://github.com/railwayapp/config).

## Why

Task 006 put the pre-deploy command (`/app/cli migrate`) in `railway.api.json`.
Railway never read that file: "Config as Code" is deprecated, services that never
used it cannot opt in since 2026-08-28, and it stops working for everyone on
2026-12-01. The dashboard shows no start command and no pre-deploy step, so
migrations do not run on deploy yet. Nothing was missed (none were pending), but
the next migration would ship without its table.

Railway's replacement is Infrastructure as Code: one file in the repo,
`.railway/railway.ts`, describes the project's services, database and settings.
A change to it is previewed as a **plan** (what Railway would change), and
applied only after that. Railway's own settings get the same treatment as code:
in git, reviewed in a pull request, with history.

## The goal

`.railway/railway.ts` describes production as it is, plus the pre-deploy
command. A pull request that changes it gets a plan comment from Railway; merging
applies exactly that plan. The next deploy logs `migrations: up to date`.

## Decisions for this step

1. **TypeScript**, the generally available language (Python and Go are beta).
   One file for the whole project, no "partials": one repo owns it all.
2. **Start from what is live, not from scratch.** `railway config pull` writes
   the current settings into the file. Then `railway config plan` must say
   "up to date" before anything is added. A clean first plan proves the file
   and production agree, so the next plan shows only what we changed.
3. **The one deliberate change in this step:** `preDeploy: "/app/cli migrate"`
   on `api`, plus `start: "/app/api"` and `healthcheck: "/health"` if the pull
   does not already have them. Nothing else changes.
4. **The database cannot be deleted by accident.** Removing a resource from the
   file deletes it on the next apply, and Postgres holds the only copy of the
   data (backups are Step 9). So:
   - the file always declares Postgres (`postgres("Postgres")`, its existing name)
   - the GitHub action runs with `confirm-destructive: false`: a plan that
     deletes anything fails to apply. A deliberate delete is done by hand, later,
     with the owner
   - locally, `apply` is never run with `--confirm-destructive`
5. **Secrets stay out of git.** The pull writes existing values as `preserve()`
   (kept on Railway, not in the file). Never `--include-variables`. References
   (`${{Postgres.DATABASE_URL}}`) are written as references, not values.
6. **Plans in pull requests, applies on merge,** with Railway's
   `railwayapp/config` action, as two workflows so the apply shows as its own run
   on `main`:
   - `railway-plan.yml`: on a pull request touching `.railway/`, posts the plan
     as a comment, marked if destructive
   - `railway-apply.yml`: on push to `main` touching `.railway/`, applies the
     plan pinned to that pull request, and nothing else. A direct push to `main`
     has no plan and fails, by design
   It authenticates with `RAILWAY_TOKEN`: a **project token scoped to
   `production`**, stored as a GitHub secret.
7. **The dashboard stops being where settings change** for anything the file
   declares. A dashboard edit makes the next apply fail (Railway sees the
   environment changed since the plan); the fix is to put the edit in the file.
   Settings the file cannot express stay in the dashboard and are listed in
   `CLAUDE.md` (see 10).
8. **`railway/iac` needs a `package.json` at the repo root.** The action
   installs the root's dependencies so the file can import it, and the repo has
   none today (`web/` has its own). A root `package.json` with one dev
   dependency, `railway`, pinned, plus its lockfile. Cloudflare Pages builds from
   `web/` and does not see it. Final layout confirmed against what
   `railway config init` scaffolds.
9. **The Railway CLI on the laptop**, for `pull` and the first `plan`
   (`brew install railway`, then `railway login` and `railway link` to the
   `rail-canvas` project, `production`). Login is the owner's: it opens a browser.
10. **What the IaC reference does not document:** watch patterns, restart
    policy, the pre-deploy timeout, Wait for CI. Whatever `pull` writes for
    them, we keep; whatever it does not, stays a dashboard setting, written down
    in `CLAUDE.md` with its value. Without watch patterns, a web-only commit also
    redeploys the API: harmless, a few cents at most.
11. **`railway.api.json` is deleted.** It never did anything, and it misleads.

## Files

| File | What |
| --- | --- |
| `.railway/railway.ts` | the project: `api` (source, start, health check, pre-deploy, variables) and `Postgres` |
| `package.json`, `pnpm-lock.yaml` (root) | only `railway`, so the file's import resolves |
| `.github/workflows/railway-plan.yml`, `railway-apply.yml` | plan on pull request, apply on merge |
| `railway.api.json` | deleted |
| `CLAUDE.md` | Railway settings live in `.railway/`; the dashboard-only list; never edit a declared setting in the dashboard |
| `.claude/STATUS.md`, task 006 | the pre-deploy step moves here from 006 |

## Owner's clicks

1. **Laptop:** `brew install railway`, then `railway login` (browser) and
   `railway link` → `rail-canvas`, `production`
2. **Railway:** Project Settings → Tokens → new token for `production`, named
   `github-iac`. Copy it once
3. **GitHub:** `rail-canvas` → Settings → Secrets and variables → Actions → new
   secret `RAILWAY_TOKEN`, paste the token
4. Optional: install the Railway GitHub App on the repo, so plan comments come
   from the Railway bot instead of github-actions

Still open from task 006, done at the end of this step: Wait for CI on, the
GitHub rule on `main`, and the practice rollback. The `plan` check is not made
required: it only runs when `.railway/` changes, and a required check that never
runs would block every other pull request.

## Order of work

1. **Owner:** click 1. **Me:** `railway config pull`, read-only. Show the owner
   the file; check every resource is there, Postgres included
2. `railway config plan`, read-only: must say up to date. If not, find out why
   before going further
3. On a branch: add the pre-deploy command, root `package.json`, the two
   workflows, delete `railway.api.json`, docs. `railway config plan` again: the
   only changes are the ones in decision 3, nothing destructive. **Show the owner**
4. **Owner:** clicks 2 and 3 (4 optional)
5. Open the pull request. The plan comment must match step 3's plan. CI's four
   checks green
6. **Owner** reads the plan comment and says go; merge. The apply run goes
   green; Railway redeploys `api`
7. Check: the dashboard shows the start command and pre-deploy step; the
   deploy log says `migrations: up to date`; `/health`; place a pixel on the site
8. Task 006's remainder: Wait for CI, the GitHub rule, the practice rollback
9. Notion: add Step 7b, tick 7 and 7b. STATUS updated

## Out of scope

Staging and PR environments (Step 16; `ctx.environment` will handle them then),
managing Cloudflare or GitHub settings as code, moving variables' values into
the file, any change to Postgres itself.

## Risks

- **The first plan is not clean** (the pull misses something, or adds a default
  the dashboard does not have). Stop and read it with the owner; never apply a
  plan we cannot explain line by line.
- **The token can change and delete anything in `production`,** the database
  included. It lives only in GitHub's encrypted secrets, is used only by the two
  workflows, never on pull requests from forks, and destructive applies are
  refused (decision 4). Rotate it if it ever leaks: delete it in Railway, make a
  new one.
