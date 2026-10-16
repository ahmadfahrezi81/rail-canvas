# Task 012 — A worker service: a job queue on Redis, thumbnails, snapshots and timelapses

**Status: approved 2026-10-17 with the owner's change (3 retries, exponential backoff) and a sanity check. Built, shown locally, pull request open.**

## Local run (2026-10-17)

API and worker on the laptop, Postgres 18 and Redis 8.2 in OrbStack, the real bucket's keys in memory (`railway run`):

- **Thumbnail:** a 200 × 100 striped PNG uploaded; `job done template:thumbnail` in the worker's log after 1.5 s; the list returned a `thumbUrl`: a 64 × 32 PNG, stripes still sharp (no blending)
- **Snapshots:** three hourly passes queued; each `job done snapshots:pass` in the worker's log (`rendered=1`)
- **Timelapse:** request → 202 `pending`; a second request → 409; 0.8 s later `ready`, 3 frames, a 256 × 256 GIF of 1,326 bytes
- **Worker down:** stopped it (`shutting down… stopped cleanly`), requested a timelapse (202, `pending`, 1 job waiting in Redis), started it again: the job ran at once (`ready`). The API's stats line then showed `queue_pending=0 queue_active=0 queue_retry=0 queue_archived=0`
- Demo objects deleted from the bucket (5 files under the demo space; the template and its thumbnail by the API's own delete)

## What changed while building

- **The timelapse job is queued after the row is committed**, not inside the transaction: a fast worker could otherwise look for a row it cannot see yet and skip it. If queueing fails, the row is deleted again
- **Pool sizes per process:** `platform.NewPostgres` takes the size; the worker's is 4

Plan: [Rail Canvas, Step 12](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Architecture" (Worker: same image, other start command), "Data model" (`templates.thumb_key`,
`snapshots`, bucket paths) and "Cost". Second stretch step. Usage checked by the owner
2026-10-17 (estimated $0.60 for the period); a small Go worker idles at about 20–30 MB, roughly
$0.30–0.50 a month.

## The goal

A third always-on Railway service, `worker`, built from the **same image** as the API and
started with `/app/worker` instead of `/app/api`. It takes jobs from a queue in Redis and does
the slow work, so the API only ever does fast things:

1. **Template thumbnails.** When an upload completes, the API queues "thumbnail template X";
   the worker makes a small PNG and fills `templates.thumb_key` (the column waits for this
   since Step 8). The Templates panel shows it
2. **Snapshot rendering.** At each full hour the API's ticker queues "take the snapshots for
   this hour" instead of rendering them itself; the worker runs the same pass as today.
   Step 13 replaces the ticker with a cron service that queues the same job
3. **Timelapses.** A member asks for one; the worker stitches the canvas's hourly snapshots
   into an animated GIF in the bucket; the member gets a link when it is ready

## Words used below

- **Job / task**: one entry on the to-do list in Redis, e.g. `{"type": "template:thumbnail",
  "templateId": "…"}`. The API *enqueues* it; the worker *handles* it
- **`asynq`**: a Go library that implements the queue on Redis: enqueueing, handing each job to
  exactly one worker, retrying failed jobs later, and scheduling. Notion's choice
- **Retry with backoff**: a failed job is tried again after a growing pause (seconds, then
  minutes), up to a limit, then kept as "archived" (dead) for a look

## Decisions for this step

### The service

1. **`/app/worker`**, a new `cmd/worker` built into the existing image by the existing
   `Dockerfile` (`go build ./cmd/...` already builds every command). Declared in
   `.railway/railway.ts` as service `worker`: same repo, `start: "/app/worker"`, Wait for CI,
   one replica in Singapore, **no public domain and no healthcheck path** (it serves no HTTP).
   Its variables are references, so nothing to paste: `DATABASE_URL` from the `api` service
   (the `app` role: the worker follows RLS like the API), the five `S3_*` from the bucket,
   `REDIS_URL` from Redis
2. **`github.com/hibiken/asynq` v0.26.0** (latest), on the Redis from Step 11. One queue,
   `default`. **Concurrency 2**: two jobs at a time is plenty, and keeps the worker's memory and
   database connections small (pool max 4)
3. **Graceful shutdown.** On `SIGTERM` (every deploy) asynq stops taking new jobs and gives
   running ones 20 seconds to finish; a job cut off is put back in the queue, not lost
4. **Jobs are safe to run twice** (idempotent), because a job can be retried after it already
   half-succeeded: a thumbnail overwrites the same key; a snapshot pass already has its
   once-per-hour claim; a timelapse writes to its own id's key
5. **Retries (owner's call): at most 3, with exponential backoff**: 10 seconds, then 20, then
   40, each with up to 20% random jitter so jobs that failed together do not all come back at
   the same instant. A job that still fails is archived and logged at error level.
   **Timeouts:** 1 minute per job (a timelapse of 240 frames takes well under)

### The three jobs

6. **`template:thumbnail {templateId, spaceId}`**, queued by `Templates.Complete` after a
   template becomes ready. The worker reads the original (at most 1 MB, at most 256 × 256:
   `Complete` already checked both, and the worker checks the header again before decoding),
   scales it down to fit **64 × 64** with nearest-neighbour (pixel art must stay sharp; a
   template already that small is kept as it is, never blown up), writes
   `spaces/{s}/templates/{id}/thumb.png`, sets `thumb_key`. The API returns a signed
   `thumbUrl` when `thumb_key` is set; the panel shows the thumbnail, or the full image until
   then. Deleting a template now deletes its thumbnail too
7. **`snapshots:pass {hour}`**, queued by the API's hourly ticker with **task id
   `snapshots:2026-10-17T05Z`**: asynq refuses a second job with the same id, and keeps a
   finished one for 2 hours (`Retention`) so the id stays taken; two API copies (a deploy
   overlap, or Step 14) queue it once. If one ever slipped through, the pass's once-per-hour
   claim in Postgres (Step 10) still makes one snapshot. The worker calls the existing
   `Snapshots.Pass`. The ticker no longer renders anything
8. **`timelapse:build {timelapseId, spaceId}`**, queued by a new endpoint. A new table
   `timelapses` (`pending` → `ready` / `failed`, RLS like every space table) records the
   request. The worker downloads the canvas's snapshots in time order (at most **240**, ten
   days of hours, the newest), decodes each (they are ours, 256 × 256, a few hundred bytes),
   and writes one animated GIF (Go's `image/gif`, 4 frames a second, the snapshots' own
   palette) to `spaces/{s}/timelapses/{c}/{id}.gif`. Fewer than 2 snapshots: `failed` with the
   reason "not enough snapshots yet". **A timelapse never stays `pending` for good:** on the
   last retry's failure the handler marks it `failed` before giving up, and a request finds
   any `pending` older than 10 minutes (a job lost with Redis's data) and marks it `failed`
   first, since the one-pending-per-canvas rule would otherwise block that canvas forever
9. **Endpoints:** `POST …/canvases/{c}/timelapses` (member; at most one `pending` per canvas,
   and a per-member limit of 5 an hour in Redis like the other limits) → 202 with the id;
   `GET …/canvases/{c}/timelapses` (newest 10, with a signed URL when ready). The frontend gets
   a "Make timelapse" button and a list with links, next to the canvas, in the same style as
   the Templates panel. The page polls the list every few seconds while one is pending

### When the queue is unavailable

10. **Postgres stays the truth, and Redis down still returns no 500** (Step 11's rule):
    - thumbnail can't be queued → logged; the template is ready anyway and shows its full
      image (a thumbnail is a nicety)
    - the hourly snapshot can't be queued → the API runs the pass itself, as today
    - a timelapse can't be queued → the request answers **503 "try again in a minute"** and
      the row is not created: the row is inserted and the job queued inside one transaction,
      which rolls back if queueing fails, so nothing half-made is left behind
    - the worker is down (deploy, crash) → jobs wait in Redis and run when it is back
11. **Queue depth in the API's stats line:** `queue_pending`, `queue_active`, `queue_retry`,
    `queue_archived`, read with asynq's inspector every 30 seconds. Added, nothing renamed.
    The worker refuses to start without `REDIS_URL` (it has nothing to do without the queue).
    The worker writes its own stats line (`service=worker`) with jobs done and failed

## Data model

```sql
CREATE TABLE timelapses (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id     uuid NOT NULL,
    canvas_id    uuid NOT NULL,
    requested_by uuid REFERENCES users (id) ON DELETE SET NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    object_key   text UNIQUE,      -- set when ready
    frames       integer,          -- set when ready
    error        text,             -- set when failed
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
-- At most one pending timelapse per canvas.
CREATE UNIQUE INDEX timelapses_one_pending ON timelapses (canvas_id) WHERE status = 'pending';
```

RLS with `FORCE`, one `tenant` policy. `templates.thumb_key` exists already.

## Files

```
api/cmd/worker/main.go              # config, pools, asynq server, handlers, stats, shutdown
api/internal/jobs/                  # job types and payloads, Enqueuer (used by services), handlers
api/internal/service/thumbnails.go  # make one thumbnail
api/internal/service/timelapses.go  # request, list, build
api/internal/service/templates.go   # queue the thumbnail; thumbUrl
api/internal/service/snapshots.go   # Run queues the pass; Pass unchanged
api/internal/platform/bucket.go     # Get (whole object, with a size cap)
api/db/migrations/…_create_timelapses.sql, api/db/queries/timelapses.sql
api/openapi.yaml                    # 0.5.0: timelapses, thumbUrl
web/src/…                           # timelapse button and list, thumbnails in Templates
.railway/railway.ts                 # service worker
.env.example, CLAUDE.md
```

A new package `internal/jobs` holds what the API and the worker share (job names and payloads).
Services depend on a small `Enqueuer` interface, so the service tests run with a fake and
need no Redis; the asynq side is tested against the real Redis.

## Tests

- **Thumbnail:** a 200 × 100 PNG becomes 64 × 32 with exactly the original's colours (nearest
  neighbour); a file whose header lies about its size is refused without decoding; running the
  job twice gives the same result
- **Timelapse:** three snapshots → a GIF of 3 frames whose first frame is the first snapshot;
  fewer than 2 → `failed` with the reason; a second request while one is pending → refused;
  a pending one older than 10 minutes does not block a new request; the last failed retry
  marks it `failed`; RLS on `timelapses`
- **Retry policy:** at most 3 retries; delays grow 10 s → 20 s → 40 s within the jitter
- **Snapshot job:** queued twice for the same hour → one job (task id); the handler takes the
  same pass as before (the Step 10 tests stay)
- **Fallbacks:** enqueue failing → thumbnail skipped without error, snapshot pass run in the
  API, timelapse 503 with no row
- **The worker end to end** (real Redis): enqueue a thumbnail job, run an asynq server in the
  test, see `thumb_key` set and the object in the fake store; a job that fails is retried
- Every existing test still passes

## Order of work

1. Build and test locally. Then show it: Postgres, Redis and the real bucket's keys (in memory,
   `railway run`) with the API **and** the worker on the laptop: upload a template and watch
   the worker log make its thumbnail; queue a snapshot pass; make a timelapse from a few
   snapshots and open the GIF. Stop the worker, queue a job, start it again: the job runs.
   Demo objects deleted afterwards
2. **One pull request**: the code and the `worker` service in `.railway/railway.ts`. Not split
   in two as before: a `worker` service declared before `/app/worker` exists would crash on
   its first deploy. The PR's plan shows one service added and nothing else. **Owner reads the
   plan; merge**
3. Check live: the `worker` deploy logs `worker started`; the API's stats line shows the queue;
   at the next full hour the snapshot job runs **in the worker's log**, not the API's; the owner
   uploads a template and sees its thumbnail; the owner makes a timelapse of pixel-club's
   canvas and opens it
4. Notion: tick Step 12. STATUS updated

## Out of scope

The cron service (Step 13: it will queue the same `snapshots:pass` job), a dashboard for the
queue (asynq has one, `asynqmon`; the stats line is enough here), more than one worker copy,
priorities between queues, timelapse options (speed, range), deleting old timelapses (Step 13's
cleanup).

## Risks

- **Worker and API deploy from the same commit at the same time**, and only the API runs the
  migrations (its pre-deploy step). A worker started before the migration would fail jobs that
  need the new table; they retry, and succeed once the API's deploy has migrated. The first
  timelapse can only be requested after the API is up anyway
- **Decoding images is the one place untrusted bytes are fully read.** Templates are checked
  twice (in `Complete` and in the job) before decoding, and capped at 1 MB and 256 × 256
- **Memory:** a 240-frame GIF holds 240 small paletted frames (about 16 MB at most) while being
  encoded; with concurrency 2 the worker stays well under 100 MB
