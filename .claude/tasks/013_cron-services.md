# Task 013 — Cron services: the hourly snapshot leaves the API, and a nightly cleanup

**Status: approved 2026-10-17 after a sanity check (pixels deleted per canvas, so the delete uses the `(canvas_id, placed_at)` index; timelapse rule spelled out; merge away from a full hour). Built, shown locally, pull request open.**

## Local run (2026-10-17)

Postgres 18 and Redis 8.2 in OrbStack, the CLI built from this branch, connected as `app`:

- **Cleanup**, on a demo database with 4 sessions expired 10 days ago and 1 live, 2,544 tickets
  expired 3 hours ago, 1 invite expired 40 days ago, 12,000 pixels placed 45 days ago and 300 today:
  `-dry-run` counted `sessions=4 tickets=2544 invites=1 pixels=12000` and deleted nothing; the real
  run deleted exactly those (12,000 pixels in three batches, 100 ms); a third run found 0 of
  everything. Left: the live session and the 300 recent pixels
- **Snapshot**, with the real bucket's keys in memory (`railway run`): `snapshot queued
  hour=09:00Z`, one task `snapshots:2026-10-17T09Z` in Redis; run again, still one task. Redis
  stopped: `snapshot not queued, taking it here`, then the pass ran in the CLI (1 canvas, skipped:
  no pixels, so nothing written to the bucket)
- Demo database dropped, Redis flushed, the local `app` login removed

## What changed while building

- **Found by the Railway plan:** the `worker` service lost "Wait for CI" when Railway created it
  (as `backup` did in Step 9). This pull request's plan puts it back
- **`cli snapshot` has no test of its own**: it needs the bucket's five variables. The queueing
  (`TestSnapshotsQueuedOncePerHour`) and the pass are tested; the fallback was shown locally
- **The healthchecks ping never logs its URL**, even in an error: Go's HTTP errors include the URL,
  so only the inner error is logged

Plan: [Rail Canvas, Step 13](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Architecture" (Scheduled jobs: hourly snapshot, nightly cleanup with `cli`), "Operations" (the
pixel history only grows) and "Cost" (cron services cost only while they run). Third stretch
step.

Checked before writing (2026-10-17): since Step 12 went live, every full hour from 19:00 to 06:00
UTC shows one `snapshots queued` in the API's log and one `job done type="snapshots:pass"` in the
worker's, none failed. The nightly backup ran at 19:02 UTC (198 KB, 13 tables).

## The goal

Today the API has a timer inside it that wakes at each full hour and queues the snapshot job.
That works with **one** API copy. Step 14 runs **two**, and each would have its own timer. The
task id makes the second queueing harmless, but a timer inside a web server is the wrong place
anyway: a deploy at 10:59 can skip an hour, and the API should only answer requests.

1. **`snapshot`**, a Railway cron service: every full hour Railway starts a container that runs
   `/app/cli snapshot` (queue this hour's `snapshots:pass` job), and it exits. The API's timer
   is deleted
2. **`cleanup`**, a second cron service: every night `/app/cli cleanup` deletes what has expired
   or is no longer needed, so tables and the bucket stop growing for nothing

Both are the **same image** as the API and the worker, with a different start command, like the
worker was in Step 12.

## Words used below

- **Cron service**: a Railway service with a schedule. Railway starts it at each scheduled time,
  it does its work and **exits**; between runs nothing runs and nothing is billed. The worker is
  the opposite: always on, waiting for jobs
- **Retention window**: how long old history is kept before the cleanup deletes it
- **Dry run**: the cleanup counts what it would delete and deletes nothing

## Decisions for this step

### `snapshot` (hourly)

1. **`/app/cli snapshot`** queues `snapshots:pass` for the current hour, with the same task id
   as today (`snapshots:2026-10-17T06Z`), and exits. The worker does the rendering, unchanged
2. **Without Redis, it takes the pass itself** (the same fallback the API has today): it renders
   in the cron container and exits. So the cron service needs the same variables as the worker:
   `DATABASE_URL` (the `app` role), `REDIS_URL`, the five `S3_*`, all references, nothing to
   paste
3. **Schedule `0 * * * *`** (UTC, every full hour). Railway starts a cron container up to a
   couple of minutes late (the backup's started at 19:02); the hour is read from the clock and
   rounded down, so a late start still takes the right hour
4. **The API's timer is deleted** (`Snapshots.Run` and its `go` line in `main`). The API keeps
   `Snapshots.List` for the snapshot endpoint. Same pull request as the cron service, so there is
   never an hour with neither, and never two

### `cleanup` (nightly)

5. **`/app/cli cleanup`**, schedule **`0 20 * * *`**: 20:00 UTC, 04:00 in Singapore, one hour
   after the backup. Last night's dump still holds everything the cleanup removes, so a mistake
   can be undone from R2 for 30 days
6. **What it deletes:**

   | What | Rule | Why |
   | --- | --- | --- |
   | `sessions` | expired, or revoked, more than 7 days ago | a dead token is useless; 7 days leaves a trail to read after a "who was logged in" question |
   | `ws_tickets` | expired more than 1 hour ago | 30-second tickets. Redis holds them since Step 11; the table is the fallback and holds about 2,544 old rows |
   | `invites` | expired more than 30 days ago (used or not) | an invite lasts 7 days; `space_members.joined_at` already records who joined |
   | `pixels` | placed more than **30 days** ago | the history only grows. The board (`canvas_pixels`) is not touched: a cell painted 40 days ago still shows. Snapshots keep the past as pictures |
   | `timelapses` | `ready` or `failed` ones beyond the newest 10 rows per canvas, and `failed` ones older than 7 days; a `pending` one never | the list shows 10. A ready one's GIF is deleted from the bucket first, then its row |
   | `templates` | `pending` for more than 1 day | an upload that never completed (tab closed after the signed URL). Its file, if any, is deleted from the bucket first, then the row |

   **Not deleted:** snapshots (about 200 bytes each, and timelapses are made from them), users,
   spaces, canvases, the board, ready templates. `link_codes` arrives in Step 15, which adds its
   own rule here
7. **The 30-day pixel window is the one real choice** (the owner's call). Nothing reads old
   history today; Step 17's load test will write a lot of it. 30 days is a constant in the code,
   easy to change
8. **Bucket first, row second.** If deleting a file fails, its row stays and tomorrow's run tries
   again. The other order would leave files no row points to, which nothing would ever find
9. **Pixels are deleted in batches** of 5,000 rows per transaction, **per canvas**, so each
   delete walks the `(canvas_id, placed_at)` index instead of scanning the table, and never holds
   a long lock while players place pixels. Each transaction sets the space as tenant (RLS), like
   everything else
10. **Connects as `app`**, not `postgres`: the cleanup follows RLS too. `app` already has
    `DELETE` on every table
11. **A dead-man's switch, like the backup's**: a second healthchecks.io check, "rail-canvas
    cleanup", daily. `HC_PING_URL` on the `cleanup` service, `preserve()`, pasted by the owner.
    Without it the cleanup still runs and logs that it is not watched. No check for the hourly
    snapshot: 24 pings a day for something the worker's log already shows
12. **The first production run is a dry run.** The first deploy starts `/app/cli cleanup
    -dry-run`: it logs what it would delete and deletes nothing. The owner reads the counts, then
    a one-line pull request drops `-dry-run`. This is the first code that deletes production
    rows on its own, so it is looked at once before it does

### Both

13. **A run cannot hang.** Railway skips the next run while one is still going, so a stuck run
    would stop all later ones. `snapshot` gives up after 5 minutes, `cleanup` after 15
14. **Small Postgres pools**: 2 connections each. They never overlap with each other in a way
    that matters (the snapshot only queues)
15. **One log line per run**, as JSON like everything else: `snapshot queued hour=…` or
    `cleanup sessions=… tickets=… invites=… pixels=… timelapses=… templates=… ms=…`. A failure
    exits non-zero, so Railway marks the run failed

## Railway

Two services in `.railway/railway.ts`:

```ts
const snapshot = service("snapshot", {
  source: github(…, { branch: "main", checkSuites: true }),
  start: "/app/cli snapshot",
  deploy: { cronSchedule: "0 * * * *", restartPolicyType: "NEVER" },
  replicas: { "asia-southeast1-eqsg3a": 1 },
  env: { DATABASE_URL: api.env.DATABASE_URL, REDIS_URL: …, S3_*: … },
});
const cleanup = service("cleanup", {
  …same source and region,
  start: "/app/cli cleanup -dry-run",
  deploy: { cronSchedule: "0 20 * * *", restartPolicyType: "NEVER" },
  env: { DATABASE_URL: api.env.DATABASE_URL, S3_*: …, HC_PING_URL: preserve() },
});
```

`restartPolicyType: "NEVER"`, as the backup has: a run that exits is finished, not crashed.

## Files

```
api/cmd/cli/main.go             # the snapshot and cleanup commands in the usage list
api/cmd/cli/cron.go             # snapshot, cleanup: config, pool, timeout, log line, healthchecks ping
api/internal/service/cleanup.go # Cleanup.Run(ctx, dryRun) → counts, one rule per function
api/internal/service/snapshots.go  # Run deleted
api/cmd/api/main.go             # the timer's go line deleted
api/db/queries/cleanup.sql      # the deletes and their counts
.railway/railway.ts             # services snapshot and cleanup
.env.example, CLAUDE.md, docs/runbook.md (running a cleanup by hand)
```

No migration, no API change, no frontend change.

## Tests

- **Each rule, both sides of the line:** an expired session 8 days ago is deleted, one 6 days ago
  stays, a live one stays; the same for tickets, invites, pixels (31 vs 29 days) and stale pending
  templates
- **The board survives:** a pixel older than 30 days that is still on the board is deleted from
  `pixels`, and `canvas_pixels` and the board read are unchanged
- **Timelapses:** 12 ready ones → the 2 oldest are gone, rows and GIFs; a failed one 8 days old is
  gone; a pending one is never touched
- **Bucket first:** with the fake store failing deletes, the timelapse and template rows stay
- **Two spaces:** each space's old pixels are deleted, nothing crosses (RLS)
- **Batches:** 12,000 old pixels go in three batches, all deleted
- **Dry run:** the counts match a real run that follows, and nothing is deleted in between
- **Twice in a row:** the second run deletes nothing
- **`cli snapshot`:** queues one job for the current hour (real Redis); with Redis down it takes
  the pass itself
- Every existing test still passes, minus the deleted timer

## Order of work

1. Build and test locally. Show it: on the laptop's Postgres, old rows of each kind made on
   purpose, `cli cleanup -dry-run` then `cli cleanup` with the counts; `cli snapshot` with the
   worker running, and with Redis stopped
2. **One pull request**: the code and both services. The plan shows two services added and
   nothing else. **Owner reads the plan; merge**
3. **Owner, after the merge** (written step by step then): a new healthchecks.io check
   "rail-canvas cleanup" (period 1 day, grace 1 hour), its ping URL pasted into the `cleanup`
   service's `HC_PING_URL` and saved in Passwords
4. Check live: the next full hour shows `snapshot queued` in the `snapshot` service's log and
   `job done` in the worker's, and **no** `snapshots queued` in the API's. At 20:00 UTC the
   dry run's counts; the owner reads them. To see it sooner: `railway cron run --service cleanup`
5. A one-line pull request drops `-dry-run`. The next night's counts match, and the second night
   deletes almost nothing
6. Notion: tick Step 13. STATUS updated

## Out of scope

Cleaning snapshots, the asynq archive (asynq keeps dead jobs 90 days by itself), `VACUUM` (Postgres's
autovacuum handles deleted rows), a cleanup endpoint or button, a healthchecks.io check for the
hourly snapshot.

## Risks

- **The pixel delete is the only big one.** At today's size it is a few thousand rows. After the
  load test it could be millions: batches keep each lock short, and the 15-minute limit stops a
  run that takes too long; the next night continues where it stopped
- **A cron container builds the whole image**, so each push to `main` now builds it four times
  (api, worker, snapshot, cleanup). Build minutes are free on Hobby; deploys take a little longer
- **Merging near a full hour can skip one snapshot**: the API's new version (no timer) may be up
  before the `snapshot` service exists. Merge away from :50–:05; a skipped hour only loses one
  picture, the next one shows the board as it is
- **Railway once ignored settings when creating a service** (Step 9: `checkSuites`, `builder`).
  After the merge, the dashboard is checked for both schedules and Wait for CI; a missing one
  gets a follow-up pull request, as in Step 9
