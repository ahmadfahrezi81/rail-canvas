# Task 016 — A staging environment that every change passes before production, with a new palette as the first release

**Status: approved 2026-10-18 (all work on a local branch, merged into `staging`, pushed with the
owner's OK; staging before production; no PR environment). Live 2026-10-18: setup released in PR #26,
the palette in PR #27 (the first release whose `staging` status came from `staging-tests.yml`).**

## The palette (2026-10-18)

`place32` (r/place 2022's 32 colours, white first because index 0 is the background) beside
`classic16`, chosen on the new-canvas form (`CreateCanvasRequest.palette`, contract 0.9.0, default
`classic16`; an unknown id is a 400). `Canvases.CreateWithPalette`; `Create` keeps the default, so
every caller stays as it was. No migration: the column already holds the id. Colour names reach the
swatches (`title`) and the bot; "gray" is read as "grey" everywhere. Tests: every palette has unique
colours and names; a `place32` canvas takes 31 and refuses 32; the staging tests gain "a 32-colour
canvas takes colour 31" (passed against a local API).

## Live on staging (2026-10-18, 14:47 UTC)

Staging at `https://api-staging-9e8a.up.railway.app`, commit `19acdb7`. The first deploy with the
passwords: `app-login: app can log in now`, `seed: created space staging-club`; later deploys:
unchanged, nothing to do, `bucket-cors: allowed https://staging.rail-canvas.pages.dev,
http://localhost:5173`. The staging tests from the laptop against it: all four pass (WebSocket and
board, cooldown 429, bot link code, template through `templates-staging` and back). Worker and
crons deployed. Production's `/health` unchanged, its plan "already up to date".

Staging's passwords were made by me (the owner's call: fake data) and put in place without being
printed: `APP_DB_PASSWORD`, `SEED_OWNER_PASSWORD` on staging's `api`, the GitHub secret
`STAGING_OWNER_PASSWORD`, `SEED_OWNER_PASSWORD` in the owner's `.env`. The domain and the
repository variable `STAGING_URL` were set from the CLI. Staging's databases and bucket have their
own names (`Postgres-staging`, `Redis-staging`, `templates-staging`): service and bucket names are
project-wide.

## Local run (2026-10-18)

- **Staging's first deploys, simulated** on an empty Postgres 18 (OrbStack): `cli predeploy` ran all
  migrations, `app-login: app can log in now`, `seed: created space staging-club with owner
  staging-owner@example.com`, bucket skipped. Run again: `migrations: up to date`, `app could already
  log in, unchanged`, `seed: users exist, nothing to do`
- **The staging tests against that API** (`STAGING_URL=http://localhost:8096`): pixel reached the
  WebSocket and the board, the second pixel got 429, a bot link code was made; the template check
  skipped (no bucket on the laptop). With the wrong commit: `/health runs commit "abc1234def5678",
  want "0000000"`; with a wrong password: `login as the seeded owner: 401`
- **`railway config plan`**: production "already up to date"; staging "8 to add": Postgres, its
  volume, Redis, the bucket, `api`, `worker`, `snapshot`, `cleanup` (no `backup`, no `bot`)
- `make check`, the database tests (with the new fresh-database test) and actionlint: green

## What changed while building

- **The first push broke staging's databases** (2026-10-18, 13:49 UTC). The apply created Postgres
  with `POSTGRES_PASSWORD` set to the literal text `secret(32, "…")` (the generator never ran;
  its spaces broke every database URL: `unexpected spaces found in password`) and Redis with no
  `REDIS_URL`. Builds passed; every service failed to start. Production untouched. Fix, after a Sync that
  brought the databases without their disks: staging was deleted and made again Empty, the owner
  added `Postgres-staging` and `Redis-staging` with **+ New → Database**, and the file adopts them
  by name (production keeps `Postgres` and `Redis`). The CLI cannot create a database in an
  environment, only edit one that exists. Also learned: service names are project-wide (a database in staging is
  an instance of the same `Postgres` service), so a new one cannot be named `Postgres`. This cost
  the owner about 20 minutes; the rule is now in CLAUDE.md (Railway as code)

- **The seed is a space, its owner and one canvas**, no extra members: the staging tests make
  their own canvas, and Step 17 adds the players. It uses `Bootstrap`, which refuses once any user
  exists, so it can only ever fill an empty database
- **Staging's apply runs the Railway CLI itself** (`railway config plan`, then `apply --yes`):
  Railway's GitHub action only applies a plan pinned to a pull request
- **The staging tests need two more things from the owner**: the GitHub secret
  `STAGING_OWNER_PASSWORD` (the same value as `SEED_OWNER_PASSWORD`) and the repository variable
  `STAGING_URL` (staging's API address)
- **The staging tests start working after the first release**: GitHub runs a `workflow_run`
  workflow from the file on `main`, so `staging-tests.yml` must be on `main` first. Until then I run
  them from the laptop
- **A staging test run is filed under `main`'s latest commit.** If one is running exactly while a
  release waits for CI, production's deploy waits for it too, and a failed staging run would skip
  it. So no push to `staging` while a release is deploying (minutes)

Plan: [Rail Canvas, Step 16](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"CI/CD and environments" (staging with its own Postgres and seeded data; one PR environment for a
single change, then deleted; staging stays until Step 17), "Cost" (staging duplicates the stack while
it runs) and "Load test" (`cli seed` refuses outside staging). Fifth stretch step.

## The goal

Today there is one Railway environment, `production`. Every change is tested on the laptop and in
CI, then goes straight to the real database and the real players.

After this step:

- **`staging`**: a second, long-lived copy of the stack (API, worker, Postgres, Redis, bucket, the
  two crons), with **its own empty database**, filled by a seed. Nothing in it can touch production.
  **Every change runs on staging, and passes tests there, before it can reach production.** Step
  17's load test runs against it too
- **The first release through it is a new palette**, `place32` (the 32 colours of r/place 2022), chosen when a
  canvas is created. It is a real feature, small, and it shows the whole path: a push to
  `staging`, the staging tests, a release pull request into `main`

## Why this exists

Real teams never test a risky change on the database their users are using. They keep:

- **Staging**: "production, but nobody cares if it breaks". Same code and settings, fake data. Used
  for things the laptop cannot show: Railway's own networking, the real build, a migration on a
  real Postgres, a load test
- **Preview (PR) environments**: one throwaway copy per pull request, so several people's changes
  can be tried at once. Useful for teams; skipped here (decision 12)

- **A gate**: production only ever receives a commit that already ran on staging and passed its
  tests there. CI on a pull request proves the code builds and its tests pass on a laptop-like
  machine; staging proves it works on the real platform, with the real network, build and database

The lessons here: **an environment is a copy of the setup, never of the data**, and **a copy of
production's settings carries production's secrets** unless you design it not to.

## Words used below

- **Environment**: Railway's word for one full copy of the project's services, each with its own
  variables, databases and domains. `production` is one; `staging` will be another
- **Base environment**: the one a PR environment is copied from. Railway's default is `production`;
  we set it to `staging`
- **Seed**: a command that fills an empty database with made-up data (a space, an owner, a canvas)

## What I checked before writing this

- **The Railway SDK can describe more than one environment from one file.** `defineRailway` gets a
  context: `ctx.environment` is the environment being planned, so `.railway/railway.ts` can say
  "in staging, one API copy and no backup". A project token is **scoped to one environment**, so
  staging needs its own token and its own plan and apply in the workflows
- **`ctx.randomString(label)` is not a secret.** It is a SHA-256 of the environment's name and the
  label (read in the SDK's source): anyone with the repo can compute it. Never used for a password
- **A PR environment copies its base's variables** (except sealed ones), and Railway lets the base
  be any environment. If the base were `production`, every PR environment would get the R2 keys,
  production's healthchecks.io URLs, the Telegram secret and `WEBHOOK_SECRET`: a PR copy's `backup`
  cron would upload its dump to our backup bucket, and its `cleanup` would ping production's check.
  So **staging must hold no production secret** (useful even without PR environments: a copy
  never inherits a secret it should not have)
- **A fresh database has no working `app` login.** The migration creates `app` without a password;
  production got one from the laptop (`make db-app-password`), which can no longer reach a database.
  Staging needs it set without the laptop (decision 8)
- **Railway has `sleepApplication`** ("App Sleeping"): a service with no traffic for about 10 minutes
  stops, and wakes on the next request. It is in the SDK's deploy settings

## Decisions for this step

### Staging

1. **`staging` is created empty in the dashboard by the owner, then filled by `.railway/railway.ts`**
   (not "Duplicate production"). A duplicate would copy every production variable, secrets included,
   and they would then have to be found and deleted one by one. An empty environment filled by the
   file starts clean, and the file says exactly what is in it
2. **What staging runs**, from the same file, branching on `ctx.environment`:

   | Resource | Production | Staging |
   | --- | --- | --- |
   | `Postgres` + volume | yes | yes, its own, empty, then seeded |
   | `Redis` | yes | yes, its own |
   | `templates` bucket | yes | yes, its own (Singapore) |
   | `api` | 2 copies | **1 copy** |
   | `worker` | 1 | 1 |
   | `snapshot` cron | hourly | hourly |
   | `cleanup` cron | 20:00 UTC, healthchecks.io | 20:00 UTC, **no ping** (it logs "not watched") |
   | `backup` cron | 19:00 UTC to R2 | **none**: staging data is fake |
   | `bot` Function | yes | **none**: one Telegram bot can only call one address |

   Step 17 raises staging's API to two copies if the load test needs it
3. **Staging's variables are written in the file, not pasted**, except one secret (decision 6).
   `DATABASE_URL` and `MIGRATE_DATABASE_URL` become Railway references in the code (`app` with
   `${{APP_DB_PASSWORD}}` at staging's Postgres; `postgres` from the Postgres service itself).
   `CORS_ORIGINS` is the Pages preview sites plus localhost. No `WEBHOOK_SECRET`: the webhook
   endpoints answer 503 there, as designed
4. **Staging comes before production** (the owner's call, 2026-10-18). Two long-lived branches:
   **`staging`** deploys to the staging environment, **`main`** to production (as today). A change
   reaches `main` only through a pull request from `staging`, after staging's tests passed on that
   exact commit. See "The release path" below
5. **Production's plan must show "no changes"** after the file learns about environments: every
   production setting stays exactly as it is. The pull request proves it with the production plan

### The release path

```
work on staging ──push──▶ CI + staging deploy + staging tests ──▶ PR staging → main ──merge──▶ production
```

- **Work happens on a local branch**, tested locally. When it is ready to try on the real platform,
  it is **merged into `staging` and `staging` is pushed, only with the owner's OK** (the owner's
  call, 2026-10-18: every push is a staging build and deploy). Every push runs CI, deploys the
  staging environment (Wait for CI), and then the staging tests
- **Local development uses staging too**: the local frontend points at staging's API
  (`VITE_API_URL`), so trying a change in the browser uses staging's data, never production's
- **Staging tests run on staging itself** (new workflow `staging-tests.yml`): once CI on a
  `staging` commit is green, it waits until staging's `/health` reports that commit (`/health` gains
  the deployed commit, `RAILWAY_GIT_COMMIT_SHA`), then runs 6–8 end-to-end checks over the real
  network against staging's public URL: log in as the seeded owner, create a canvas, place a pixel,
  see it on the board and on a WebSocket, the cooldown, the bot link code, a template upload. It
  sets a commit status **`staging`** on that commit: green or red
- **It must not start on `push`** (found in the sanity check): Railway's Wait for CI holds a deploy
  "until every GitHub Actions check suite on the commit has finished". A test run started by the
  push would wait for the deploy while the deploy waits for it, until Railway gives up after two
  hours. So it starts on `workflow_run` (after CI), which GitHub files under `main`'s latest commit,
  not the `staging` commit, and it reports with a commit status, which Wait for CI does not read
- **Release: a pull request from `staging` into `main`.** `main`'s ruleset gains one required check,
  `staging`, beside the four CI checks: a commit that failed on staging cannot reach production
- **Releases merge with a merge commit, not squash.** A squash would give `main` a commit that
  `staging` never had; the next release would show old changes again and conflict. With a merge
  commit both branches share history. `main`'s ruleset changes from "squash only" to "merge commit"
- **The full-hour rule stays on `main`**: a release redeploys production and its crons, so releases
  stay away from :50–:05 UTC. Pushes to `staging` touch only staging

### The testing cycle

Every real test runs once per change; the repeats are cheap and kept only where they are the
standard gate (the owner's call: safe, and as little to maintain as possible).

| Stage | What runs | The question it answers |
| --- | --- | --- |
| Laptop | `make check` (plus `make test-db` when SQL changed) | "Did I just break something?", in seconds |
| Push to `staging` | full CI: unit, database and Redis tests, contract, builds | "Is the code correct?", on a clean machine |
| Staging deployed | the staging tests, 6–8 checks | "Do the pieces work together on the real platform?" |
| Release PR `staging` → `main` | **nothing new**: the PR's commit is the one just checked, so its CI results and `staging` status count | (already answered) |
| Merge into `main` | full CI again, unchanged | the gate Railway's Wait for CI reads before production deploys; a merge is a new commit |
| Production deployed | Railway's healthcheck (`/health` must answer before traffic moves); I check `/health` shows the release's commit | "Is it up, and is it the new version?" Read-only: **nothing is ever tested by writing to production** |

- **CI stops running on pull requests** (`ci.yml` keeps `push` on `main` and `staging`). Required
  checks are tied to a commit, and the release PR's commit already has them from its push
- **Detail lives in unit and database tests** (fast, point at the line); the staging tests stay few
  and only prove the wiring. A new feature adds a staging check only when it adds real wiring (a new
  service, a new outside call)
- **No production smoke workflow**: started by the merge, it would be a check suite on the commit
  production waits for, the same deadlock as above. Railway's healthcheck already refuses a version
  that does not answer

### The workflows

6. **Plan and apply follow the branches.** A release pull request into `main` gets
   **production's** plan, as now, applied on merge. Staging's settings are applied **on every push
   to `staging`** with `RAILWAY_TOKEN_STAGING` (a project token for `staging` made by the owner): a
   push has no pull request and so no pinned plan, so the job plans and applies in one go, still
   refusing anything destructive. It runs on `push`, so staging's deploy waits for it: settings land
   before the new code. A Railway change is therefore tried on staging before production
7. **Railway drops Wait for CI when it creates a service** (learned in Step 13), so after the
   pull request that creates staging, a second small one applies it. Same as before

### The `app` login and the seed

8. **The pre-deploy command gives `app` its password when it has none.** `cli predeploy` gains one
   step, before `migrate` finishes: if the `app` role cannot log in yet and `APP_DB_PASSWORD` is set,
   `ALTER ROLE app WITH LOGIN PASSWORD …` (as `postgres`, value passed as a parameter, never logged).
   Production's `app` can already log in, so **production skips it**: nothing changes there. A
   fresh staging or PR database gets its login on its first deploy, with no laptop
9. **`APP_DB_PASSWORD` on staging is the one value the owner pastes** (`openssl rand -hex 32`, saved
   in Passwords as "rail-canvas staging app db"), declared `preserve()`. Not a production value: a
   leaked staging password opens only fake data
10. **`cli seed`** fills an empty database: a space `staging-club`, an owner
    `staging-owner@example.com`, two members, a canvas on `classic16`. It runs from the pre-deploy
    command, after the migrations, **only when `SEED_OWNER_PASSWORD` is set**, and does nothing if
    the space exists (so every deploy can run it). It **refuses when `RAILWAY_ENVIRONMENT_NAME` is
    `production`**, whatever the variables say. The owner's password is the second pasted value
    ("rail-canvas staging owner" in Passwords). Step 17 adds the 200 players to it
11. **A new environment therefore works by itself**: its first deploy on an empty database sets
    the `app` login, migrates and seeds. Staging needs no laptop step, and neither would another
    environment later (a PR environment, or a rebuilt staging)

### The PR environment: skipped

12. **Not built** (the owner's way of working, 2026-10-18). A PR environment is a throwaway copy of
    the backend per pull request, made so several people's branches can be tried at once. With one
    developer working on `staging`, staging already is that copy, and the only pull requests are
    releases, already tested on staging. Notion's Step 16 is updated to say so. The pre-deploy
    `app` login and seed (above) still make any future environment work by itself

### The palette

15. **`place32`**, the 32 colours of r/place 2022, with names for the bot (white, lightgrey, grey,
    black, …, the 2022 additions such as burgundy, teal, lavender). Defined in Go beside
    `classic16`; **no migration** (a canvas stores the palette's id, the column is text)
16. **Chosen when a canvas is created**: `CreateCanvasRequest.palette`, optional, default
    `classic16`; an unknown id is a 400. The board stays one byte per cell, 32 colours fit. Existing
    canvases never change palette
17. **Web**: the "new canvas" form gets a palette choice; the colour picker already draws whatever
    the canvas's palette holds (it wraps to two rows)
18. **The bot needs nothing new**: `Palette.Color` reads the canvas's own palette and names

### Pages

19. **Pages preview sites talk to staging.** Pages builds every push to `staging` as a preview,
    always at **`staging.rail-canvas.pages.dev`** (the branch's fixed address). The owner sets Pages'
    **Preview** variable `VITE_API_URL` to staging's API domain; Production keeps production's. A
    preview site then never writes to the real board. Staging's `CORS_ORIGINS` names that exact
    address and localhost, so template uploads work there too (only wildcard origins fail, Step 8).
    Pages does not wait for CI; it is only the test site

## How the palette travels

1. Committed on `staging`, pushed. CI, staging deploys, the staging tests run (with a new one for
   `place32`) and turn the commit green. The owner can try it in the browser (local or a Pages
   preview, both pointed at staging)
2. Release pull request `staging` → `main`; production's plan says "no changes"; merge (merge
   commit, away from :50–:05). Production deploys. Check: a `place32` canvas can be created; old
   canvases unchanged

## Files

```
.railway/railway.ts                  # ctx.environment: staging's resources and variables
.github/workflows/railway-plan.yml   # into main: production's plan; into staging: staging's
.github/workflows/railway-apply.yml  # push to main: production (pinned plan); push to staging: staging
.github/workflows/ci.yml             # push to main and staging; no longer on pull requests
.github/workflows/staging-tests.yml  # workflow_run after CI: waits for staging, tests it, sets "staging"
api/internal/handler/health.go       # /health reports the deployed commit
api/stagingtest/                     # the staging tests (Go, build tag "staging", real URL)
api/cmd/cli/main.go, migrate.go      # predeploy: app login when missing, then seed
api/cmd/cli/seed.go                  # cli seed (new)
api/internal/service/palette.go      # place32
api/internal/service/canvases.go     # Create takes a palette
api/openapi.yaml                     # 0.8.0: CreateCanvasRequest.palette, Palette ids listed
web/src/components/…                 # palette choice on the new-canvas form
CLAUDE.md (Environments), docs/runbook.md, .env.example
```

## Tests

- **`app` login**: on a database where `app` cannot log in, `predeploy` makes it log in with
  `APP_DB_PASSWORD`; run again, it changes nothing; where `app` already logs in, it is left alone
- **Seed**: an empty database gets the space, owner, members and canvas; a second run adds nothing;
  `RAILWAY_ENVIRONMENT_NAME=production` refuses, with or without `SEED_OWNER_PASSWORD`
- **Palette**: `place32` has 32 colours and 32 distinct names; a canvas created with it reports it,
  takes colour 31 and refuses 32; an unknown palette id is a 400; no palette means `classic16`
- **`/health`** reports the commit (empty on the laptop)
- **The staging tests** run against a local API too (`STAGING_URL=http://localhost:8080`), so they
  are written and debugged before staging exists; on staging they leave only rows in the seeded
  space (a canvas per run, named with the commit, so a reader can tell runs apart)
- **The Railway file**: `railway config plan` locally (read-only) for both environments; production
  shows no changes
- Every existing test still passes

## Order of work

1. **Owner, in the dashboard and GitHub** (about 10 minutes, list below): staging created empty, its
   project token, the GitHub secret, the `staging` branch. Nothing else yet
2. Build and test locally: predeploy and seed against a throwaway Postgres, the staging tests
   against a local API, `railway config plan` for both environments. Then **the first push to
   `staging`**: the Railway file, workflows, `cli`, `/health`, the staging tests, docs. CI runs;
   staging's apply creates staging's services
3. Owner pastes `APP_DB_PASSWORD` and `SEED_OWNER_PASSWORD` on staging's `api` and generates its
   domain (the first deploy fails until then: no password for `app`); the next push applies Wait
   for CI to the new services. Check: staging answers `/health`
   with the commit, the seeded owner can log in, the staging tests turn the commit green, a pixel
   placed there is not on production's board
4. **First release** PR `staging` → `main` (this step's setup): production's plan must say "no
   changes"; owner merges with a merge commit. Then the owner changes `main`'s ruleset (require
   `staging`, allow merge commits), sets Pages' preview `VITE_API_URL`; local `web/.env.local`
   points at staging
5. **The palette**, the whole path (above): the first release that the `staging` check guards
6. Notion: tick Step 16 (with the PR environment skipped, and why) and note the release path.
   CLAUDE.md (CI and deploying, Railway as code, how work flows) and STATUS updated

## Owner's clicks (after approval)

1. Railway → environment menu → **New Environment** → **Empty Environment**, named `staging`
2. Railway → Project Settings → Tokens → new project token for **`staging`**, named `github-iac-staging`
3. GitHub → repo Settings → Secrets → Actions → new secret `RAILWAY_TOKEN_STAGING` with that token
4. GitHub → create the branch `staging` from `main` (Branches → New branch)
5. Later: two pastes, Generate Domain, `main`'s ruleset (the `staging` check, merge commits), the
   Pages preview variable. I will list them again when we get there

## Out of scope

PR environments (decision 12), copying production data into staging, the 200 load-test players
(Step 17), a Telegram bot for staging, backups of staging, automatic releases (a person merges them).

## Risks

- **Staging becomes part of the release path**, so it can no longer be deleted after Step 17 as
  Notion planned: it lives as long as the project. A push rebuilds and replaces its API and worker;
  it never makes a new backend (the database and Redis keep running). Cost: about \$3 a month more (Postgres, Redis,
  API, worker); the owner is fine with it. The usage limit (\$7) still stops **every** service when
  reached, production included: glance at the Usage page after a few days
- **`workflow_run` is the one part I have not seen work yet.** If its run still holds staging's
  deploy, the fallback is a `deployment_status` trigger, or the tests polling on a schedule; found
  on the first push, before anything relies on it
- **Railway lists no charge for building** (its bill shows only CPU, memory, disk and network), so a
  push costs the minutes staging runs, not the build
- **A broken staging blocks production**, by design. If staging itself breaks (not the code), the
  fix is on staging; the ruleset is not loosened to get around it
- **The staging tests write to staging's database**: a canvas and a few pixels per run. Staging's
  nightly cleanup handles history; canvases accumulate slowly (one per merge) and are harmless
- **Staging's settings apply without a pinned plan** (plan and apply in one job on push). A
  dashboard change on staging is overwritten by the next push, which is the point of declaring it.
  Production keeps the stricter pinned plan. If Railway's action cannot apply without a pull
  request, the job runs the Railway CLI (`railway config apply --yes`) itself
- **The SDK's environment support is beta.** If an empty environment cannot be filled by the file
  (found by the local plan in step 2), the fallback is "Duplicate production" and then deleting the
  copied secrets and the `backup` and `bot` services by hand, listed in this file
- **`main` gets merge commits** (releases); its ruleset changes from "squash only" to allow them.
  `staging`'s history is every commit as written, so commit messages keep the usual
  `type(scope): sentence` form
- **Staging's database is private**, like production's: the laptop reaches staging through its API,
  not its Postgres. A public address on staging's database (fake data) is possible later if local
  Go runs against it are wanted; not part of this step
