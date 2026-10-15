# Task 010 — An hourly canvas snapshot, and one webhook

**Status: live 2026-10-16 (PR #11). The merge deploy applied `create_snapshots`; `WEBHOOK_SECRET` set by the owner (64 characters), `webhook enabled=true`. Live webhook: wrong secret 401, right secret 201 (pixel 8, blue, at 128, 128 on pixel-club's canvas). First live pass at 17:00 UTC: `canvases=1 rendered=1 failed=0 ms=946`; `2026-10-15T17Z.png`, 203 bytes, nine painted cells matching the board (the owner's red pixel beside the webhook's blue one), listed with a signed URL.**

## Local run (2026-10-15)

The API on the laptop against a throwaway `postgres:18`, with the real bucket's keys from `railway run` (in memory):

- Webhook: 16 pixels of a red heart placed (201, `source = 'bot'`); at once again → 429 path covered by tests, wrong or missing secret → 401, a user who is not a member → 404, x = 300 → 400
- Snapshot through a throwaway program calling `Pass` (deleted, never committed): pass 1 rendered, pass 2 in the same hour skipped, pass 3 an hour later with no new pixel skipped. The list endpoint returned it with a signed URL (401 without a login); the PNG is 256 × 256, 4-bit palette, **200 bytes**, and shows the heart. Deleted from the bucket afterwards
- **Changed from the plan:** the key's time had colons (`…T16:00:00Z.png`), which URLs escape and some tools refuse; it is now `2026-10-15T16Z.png`
- **`preserve()` does not create a variable.** The plan for `WEBHOOK_SECRET: preserve()` showed no change: it only keeps a value that exists. The owner creates the variable in the dashboard (as the backup's were)

Plan: [Rail Canvas, Step 10](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Data model" (`snapshots`, bucket paths), "Security and tenancy" (Bot) and Steps 12–15, which
build on this one.

## The goal

Two small, separate features:

1. **Snapshots.** Once an hour the API takes a picture of every canvas that changed: a PNG,
   one image pixel per cell, saved to the bucket and recorded in a new `snapshots` table. A
   member can list a canvas's snapshots. They are the raw material for Step 12's timelapses
2. **A webhook.** `POST /webhooks/spaces/{spaceId}/canvases/{canvasId}/pixels` places a pixel
   for another *program*, not a browser: in Step 15 a Discord or Telegram bot's Function will
   call it. Instead of a login token, the caller proves itself with a **shared secret**, a long
   random password that only the API and that program know

## Words used below

- **Ticker**: a timer inside the running API that fires every hour, like a cron job but living
  in the API's own process. Step 13 replaces it with a real Railway cron service
- **Webhook**: an address one program calls to tell another that something happened. Here:
  "user X wants pixel (10, 20) in red"
- **Shared secret**: a random string set as a Railway variable on the API and, later, on the
  bot's Function. A request without it is refused

## Decisions for this step

### Snapshots

1. **An in-process ticker in the API**, started in `main` beside the stats line. It fires at
   the **top of every hour (UTC)**, not "60 minutes after boot", so a deploy does not shift it
   or add an extra snapshot. Off when the API has no bucket (`S3_*` unset), like templates.
   Notion's plan: Step 13 moves it to a cron service, and it must be gone before Step 14's two
   replicas, or each replica would snapshot. It is a timer that, after each pass, waits for
   the next full hour: a slow pass delays the next one instead of overlapping it
2. **One pass:** list every space (the `spaces` table has no RLS: the one place a background job
   reads across tenants), then, per space inside `inTenant`, each canvas. **Per canvas, one
   transaction:** read its cells *with* their `pixel_id` (a new query; `Board`'s query has no
   `pixel_id`), so the picture and its newest `pixel_id` come from the same read and cannot
   disagree; claim the row; render; upload; commit. A canvas with no pixels yet is skipped
3. **Skip a canvas that did not change.** The `snapshots` row stores `last_pixel_id`, the newest
   pixel the picture includes. If the canvas's newest `pixel_id` is the same as in its last
   snapshot, nothing is rendered. A quiet canvas costs one query an hour, not one PNG
4. **One snapshot per canvas per hour, even when two API versions run at once** (a deploy
   overlaps the old and the new version for a moment). `taken_at` is the hour, truncated, and
   `UNIQUE (canvas_id, taken_at)`. In the canvas's transaction: insert the row (a conflict
   means the other version already has it: skip), upload, commit. A failed upload rolls the row back, so
   the next hour tries again
5. **The PNG is 256 × 256, one pixel per cell, indexed colors** (Go's `image.Paletted` with the
   canvas's palette). Paletted PNGs of a mostly empty board are a few KB. Scaling up is the
   viewer's job (CSS `image-rendering: pixelated`), so the stored file stays the exact board
6. **Bucket key** `spaces/{space}/snapshots/{canvas}/{2026-10-15T16Z}.png`, built by
   `service.SnapshotKey` from server values only, as for templates. Same private `templates`
   bucket (Notion: "PNG snapshots of the canvas" share it). `ObjectStore` gains `Put`, used by
   the server for its own small files; browsers still upload with signed URLs
7. **`GET /spaces/{spaceId}/canvases/{canvasId}/snapshots`**: members only, newest first, the
   last 48 (two days), each with a download URL signed for an hour. Freshness: live
8. **One log line per pass:** `msg=snapshots canvases=… rendered=… skipped=… ms=…`, and one
   error line per canvas that failed (the others still run). The stats line is unchanged
9. **No retention yet.** At a few KB an hour per changed canvas, a year is tens of MB. Step 13's
   nightly cleanup can thin them out

### Webhook

10. **Its own path, outside the browser routes:** `POST /webhooks/spaces/{spaceId}/canvases/{canvasId}/pixels`,
    body `{userId, x, y, color}`, in `openapi.yaml` like every other endpoint. The space and
    canvas are in the path, as everywhere else
11. **The secret is checked first, in constant time.** Header `X-Webhook-Secret` against
    `WEBHOOK_SECRET`. Both are hashed with SHA-256 and compared with
    `subtle.ConstantTimeCompare`, so neither the length nor the first wrong character leaks
    through timing. Wrong or missing: 401, logged without the value. `WEBHOOK_SECRET` unset:
    503, the feature is off. Set but shorter than 32 characters: the API refuses to start
12. **Who places the pixel: the caller says, by `userId`.** The secret proves the caller is our
    own program, and that program is trusted to name the user. The user must still be a member
    of the space and still waits out the cooldown: the webhook goes through the same
    `Pixels.Place` as the browser, with `source = 'bot'`, and the pixel appears live for
    everyone. **A suspended user is refused** (404, like a stranger): the browser path gets
    that from the live session check, which the webhook skips, so the webhook checks
    `users.status = 'active'` itself. **Step 15 changes this:** the bot will send its platform's user id, and the API
    looks it up in `bot_links` after the user linked their account with a one-time code. Until
    then nothing but us holds the secret
13. **No rate limit beyond the cooldown.** A 32-byte random secret cannot be guessed, and each
    user can place one pixel per cooldown however they arrive. Responses: 201 placed, 400 bad
    pixel, 401 bad secret, 404 canvas not found or user not a member, 429 cooldown, 503 off
14. **`WEBHOOK_SECRET` is declared in `.railway/railway.ts` with `preserve()`**; the owner
    generates the value (`openssl rand -hex 32`) and pastes it into Railway. In `.env.example`
    with a comment

## Data model

```sql
CREATE TABLE snapshots (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id      uuid NOT NULL,
    canvas_id     uuid NOT NULL,
    object_key    text NOT NULL UNIQUE,
    -- Newest pixel in the picture: an unchanged canvas is not snapshotted again.
    last_pixel_id bigint NOT NULL,
    taken_at      timestamptz NOT NULL,   -- the hour, truncated
    UNIQUE (canvas_id, taken_at),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
```

RLS with `FORCE`, one `tenant` policy, as for `templates`. Index on `(canvas_id, taken_at DESC)`
comes with the unique constraint. Queries: newest pixel id of a canvas, latest snapshot of a
canvas, claim (insert … on conflict do nothing returning), list the last 48, list all spaces.

## Files

```
api/db/migrations/…_create_snapshots.sql
api/db/queries/snapshots.sql
api/openapi.yaml                      # 0.4.0: list snapshots, the webhook
api/internal/service/snapshots.go     # Run (the ticker), Pass, render, SnapshotKey, List
api/internal/service/templates.go     # ObjectStore gains Put
api/internal/platform/bucket.go       # Put
api/internal/handler/snapshots.go
api/internal/handler/webhooks.go
api/internal/config/config.go         # WEBHOOK_SECRET
api/cmd/api/main.go                   # start the ticker
.railway/railway.ts, .env.example, CLAUDE.md
```

## Tests

Database tests (as `app`), with the fake object store:

- render: a board with known cells gives a 256 × 256 PNG whose pixels have the palette's colors
- a pass snapshots a changed canvas, skips it the next time if nothing changed, and snapshots it
  again after one more pixel
- two passes in the same hour make one row and one upload (the deploy overlap)
- a failed upload leaves no row
- list: newest first, at most 48, only this canvas, signed URLs; another space's canvas: not found
- RLS on `snapshots`: no tenant, no rows
- the object key always starts with `spaces/{spaceId}/snapshots/`

Handler tests (no database needed for these):

- webhook: no header, a wrong secret, a secret differing only in the last character → 401;
  secret unset → 503
- webhook with the right secret places a pixel with `source = 'bot'` for a member; a non-member
  → 404; a suspended member → 404; twice in a row → 429

## Owner's clicks

After the pull request merges:

1. In your terminal: `openssl rand -hex 32 | pbcopy` (makes a random secret and copies it;
   nothing is shown)
2. Railway → `api` → Variables → **New Variable**, name `WEBHOOK_SECRET`, paste. Also save it in Passwords as
   "rail-canvas webhook secret": Step 15's Function will need it

Until then the webhook answers 503 and everything else works.

## Order of work

1. One pull request: migration, queries, `openapi.yaml`, service, handlers, config, ticker,
   tests, `.railway/railway.ts` (`WEBHOOK_SECRET: preserve()`), docs. CI green; the PR's plan
   shows one variable added on `api`, nothing else
2. Show locally first: the API on the laptop against a throwaway Postgres in OrbStack, with the
   bucket keys from `railway run` (in memory). For the snapshot, a throwaway program in the
   scratchpad (never committed) calls the same exported `Pass` once, so no demo-only switch
   goes into production code; one PNG under the local space's own path in the bucket, opened,
   then deleted; the webhook placing a pixel that appears live in the
   browser
3. Merge. The migration runs in the pre-deploy step
4. Owner sets `WEBHOOK_SECRET` (above)
5. Check live: at the next full hour the log shows `msg=snapshots … rendered=1`, the bucket has
   `spaces/{pixel-club}/snapshots/{canvas}/….png`, and the list endpoint returns it. The hour
   after, with no new pixels: `skipped=1`. The webhook, called through `railway run -s api`
   (the secret stays in memory) for the owner's user: **one real pixel on pixel-club's
   canvas, placed on purpose**; it appears on the site, and a wrong secret gets 401
6. Notion: tick Step 10. STATUS updated

## Out of scope

A snapshot gallery or timelapse in the frontend (Step 12 builds timelapses; the list endpoint is
there for it), deleting old snapshots (Step 13), the bot and account linking (Step 15), signing
webhook bodies (HMAC) instead of a header secret (worth it when the caller is someone else's
server; here both ends are ours), moving the ticker to cron (Step 13).

## Risks

- **A slow pass delays the next one.** Not at this size (one canvas renders in milliseconds);
  the log line's `ms` shows how long passes take
- **Uploading inside the transaction holds a database connection** for the upload's duration
  (a few hundred ms, one canvas at a time). Fine with a pool of 10 and one canvas; revisit when
  the worker takes over rendering in Step 12
- **A secret in a header can end up in logs** if a proxy logs headers. Our request log does not
  log headers; Railway's edge does not show them in the log view
