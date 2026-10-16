# Task 011 — Redis: a board cache, the cooldown, limits and tickets, and still working without it

**Status: approved 2026-10-17, after a sanity check (cooldown per canvas, rebuild writes `ver`). PR #13 (Redis) merged: Redis 8.2, `redis.railway.internal`, no TCP proxy (the template adds its own volume). PR 2 open.**

## What changed while building

- **A circuit breaker** (`Redis.Up`, 5 seconds). The first tests with Redis down took 0.5–1.7 s *per call*: the client retried a dead Redis, and one request makes several calls. Now a failure makes the API skip Redis for 5 seconds, client retries are off (`MaxRetries: -1`), and the "Redis down" test went from 4.6 s to 0.6 s
- **Stale boards after an outage:** a placement made while Redis is skipped cannot update the cached board, so the board key is remembered as stale and deleted before its next read (or at the next stats ping). Tested
- **The cooldown check runs right after the canvas lookup**, inside the transaction, because it needs the canvas's cooldown. A too-soon click still never reaches the Postgres claim or any write; it does read the canvas row

## Local run (2026-10-17)

API on the laptop, Postgres 18 and Redis 8.2 in OrbStack:

- Redis up: cold board read 12 ms (rebuild), warm 3 ms; a webhook pixel appears in the cached board; a WebSocket ticket goes to Redis (0 rows in `ws_tickets`)
- Redis frozen (`docker pause`, so every call hangs to its timeout): placing works (201), the board is read from Postgres and shows the new pixel, a ticket goes to Postgres (1 row), the cooldown comes from Postgres (201, then 429). One warning line in the log, `redis unavailable, using fallbacks`
- Redis back: the board read shows the pixels placed during the outage (the stale board was dropped and rebuilt)
- Stats line: `redis_ok=true redis_errors=1 board_hits=2 board_misses=2`

Plan: [Rail Canvas, Step 11](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Data model" (Redis keys, "A placement, in order"), "Security and tenancy" (Rate limits) and
"Cost". First stretch step. Cost checked with the owner 2026-10-17: the project's estimated
usage was $0.60 for the period, so an always-on Redis (Notion: about $0.50–1 a month) fits.

## The goal

Add Railway Redis, reachable only on the private network, and move four things into it:

1. **The board**, so loading a canvas is one Redis read instead of a Postgres scan
2. **The cooldown check**, so a too-soon click is refused without touching Postgres
3. **The login and join rate limits**, so they hold across more than one API copy (Step 14)
4. **WebSocket tickets**, which expire by themselves instead of piling up in `ws_tickets`

And the rule that makes it safe: **Postgres stays the source of truth, and when Redis is down
nothing returns a 500.** Every Redis use has a Postgres or in-memory fallback.

## Words used below

- **Redis**: a database that keeps everything in memory, so reads take well under a
  millisecond. Here it only holds copies and short-lived things; losing it loses nothing
- **Cache**: a fast copy of data whose real home is elsewhere (Postgres). It can be thrown
  away and rebuilt at any time
- **`BITFIELD`**: a Redis command that treats one stored string as an array of small numbers.
  The board is 65,536 cells of one byte each, so the whole board is one 64 KB string, and a
  single cell can be changed in place
- **Lua script**: a few lines run *inside* Redis, all at once, with nothing else running in
  between. That is how "compare, then set" becomes one step that cannot race
- **Single-flight**: when many requests miss the cache at the same moment, one of them
  rebuilds it and the others wait for that result, instead of all hitting Postgres at once

## Redis keys

All per space, as the rule in CLAUDE.md says, except the two that belong to a person, not a
space (Notion's key list):

| Key | Type | What | Expires |
| --- | --- | --- | --- |
| `space:{s}:canvas:{c}:board` | string, 1 byte per cell | the board | 10 minutes after a rebuild |
| `space:{s}:canvas:{c}:ver` | hash, cell → `pixel_id` and color | the newest pixel known per cell | with the board |
| `space:{s}:canvas:{c}:lock` | string | the rebuild lock (single-flight across API copies) | 5 seconds |
| `space:{s}:cooldown:{user}` | string, when the member last placed | the cooldown check | 1 hour (the longest cooldown) |
| `rate:login:{email}\|{ip}`, `rate:login-ip:{ip}`, `rate:signup:{ip}`, `rate:join:{ip}`, `rate:template:{user}` | rate-limit state (the `rate:` prefix is the library's) | attempts | by themselves |
| `ticket:{sha256}` | string, the user id | a WebSocket ticket | 30 seconds |

## Decisions for this step

### Infrastructure

1. **Redis is declared in `.railway/railway.ts`** (`redis("Redis", { region })`, the SDK has it),
   in Singapore beside the API, **private network only**: no public TCP proxy, as for
   Postgres. The `api` service gets `REDIS_URL` as a reference. Its own pull request first, so
   the plan shows exactly one database added and one variable, and nothing else. Railway's
   Redis template may come with a TCP proxy or a volume; the plan shows it, and anything
   public is removed before merging
2. **Go client: `github.com/redis/go-redis/v9`**, the official one. Short timeouts (dial
   500 ms, read and write 200 ms): a slow Redis must not make a request slow; past the timeout
   the fallback runs. Connection pool kept small (10), like Postgres's
3. **`REDIS_URL` is optional.** Unset (local runs, the existing tests), the API behaves exactly
   as today. Set but Redis unreachable at boot: the API still starts (the client connects
   lazily) and runs on its fallbacks. CI's `api` job gets a `redis` service container beside Postgres, so the Redis
   tests run on every pull request

### The board

4. **Read:** `GET` the board key. Present: that is the board, sent as it is. Missing: rebuild
   (below). Redis failing: read from Postgres as today (`Canvases.Board`). Freshness comment:
   "Redis copy, kept current by every placement; rebuilt at most every 10 minutes"
5. **Write, after each placement's commit** (never inside the transaction), one Lua script,
   the compare-and-set from Notion: if the cell's `pixel_id` in `ver` is newer than this
   pixel's, do nothing; otherwise record this `pixel_id` and color in `ver`, and if the board
   exists, set that one byte with `BITFIELD`. So two placements on one cell, finishing in
   either order, always leave the newer color
6. **If that write fails** (Redis down or slow): delete the board key, so the next read
   rebuilds from Postgres. If even the delete fails, the 10-minute expiry bounds how long a
   cell can be wrong, and players already watching get the pixel over the WebSocket anyway
7. **Rebuild, single-flight:** inside one API copy, Go's `singleflight` turns many concurrent
   misses into one; across copies, a 5-second `SET … NX` lock does the same, and the losers
   wait briefly (up to 1 second) for the board to appear, then read Postgres directly. The
   rebuild reads the cells with their `pixel_id` (Step 10's query), then a second Lua script
   writes the board **merging with `ver`**: for each cell the newer of Postgres's pixel and
   `ver`'s wins, and Postgres's newer ids are written into `ver` too. That closes the gap
   where a pixel commits after the rebuild's read but before its write, and a late write of
   an older pixel can no longer win over a cell the rebuild filled. A board key is only ever
   created by a rebuild, which first found the canvas in that space through Postgres
8. **Why expire the board every 10 minutes:** it costs one Postgres read per canvas per 10
   minutes, and it guarantees that any drift (a missed write, a Redis restart mid-update)
   heals by itself. Without an expiry, a wrong cell could stay wrong forever

### The cooldown

9. **A small Lua claim before the transaction.** The key holds when the member last placed
   (Redis's own clock). Canvases in one space can have different cooldowns while the member's
   turn is per space (as `last_placed_at` is), so a plain `SET NX EX <cooldown>` would be
   wrong: after a 60-second canvas it would also block a 10-second one for 60 seconds. The
   script compares the stored time with *this* canvas's cooldown: too soon → refused at once
   with the time left, and Postgres is not touched; otherwise it stores now and lets the
   placement through. Set → the transaction runs as today, **including the Postgres
   claim**, which stays the real rule (it is what the tests of five simultaneous clicks
   prove). If the transaction fails, the key is deleted so a failed attempt does not cost
   the member their turn. If Postgres says "too soon" although Redis let it through (Redis
   restarted and forgot), the key's expiry is set to Postgres's time left, so Redis never
   blocks longer than the real rule. Redis failing → straight to the Postgres claim, as today

### Limits and tickets

10. **Rate limits in Redis**, same numbers as now, with `github.com/go-redis/redis_rate/v10`
    (the same token-bucket idea as today's `golang.org/x/time/rate`, but the state lives in
    Redis, so every API copy shares it). Redis failing → today's in-memory limiter. The
    template-upload limit moves too
11. **Tickets in Redis:** created with `SET ticket:{hash} {user} EX 30`, redeemed with
    `GETDEL`, which reads and deletes in one step, so a ticket works once even under a race
    (as the Postgres `UPDATE` does today). Redis failing on create → the ticket goes to
    Postgres as today; on redeem, Redis is tried first, then Postgres. `ws_tickets` stays as
    the fallback; Step 13's cleanup empties it

### Watching it

12. **The stats line gains** `redis_ok` (1 or 0, a `PING` every 30 seconds), `redis_errors`
    (calls that fell back since boot), `board_hits` and `board_misses`. Added, nothing
    renamed. A Redis failure is logged once a minute at most, not once per request

## Files

```
.railway/railway.ts                       # Redis, REDIS_URL on api (PR 1)
api/internal/platform/redis.go            # client, timeouts, PING
api/internal/service/boardcache.go        # read, CAS write, rebuild with lock + merge (Lua)
api/internal/service/cooldown.go          # NX EX claim and release
api/internal/service/canvases.go, pixels.go, auth.go   # use them, with fallbacks
api/internal/ratelimit/                   # Redis-backed limiter, in-memory fallback
api/internal/config/config.go             # REDIS_URL
api/cmd/api/main.go                       # wiring, stats fields
.github/workflows/ci.yml                  # redis service container for the api job
.env.example, CLAUDE.md
```

## Tests

With a real Redis (`TEST_REDIS_URL`, a container locally and in CI) and the test Postgres:

- **CAS:** writing pixel 7 then pixel 5 on one cell leaves 7's color; 5 then 7 also leaves 7
- **Rebuild merge:** `ver` holds a pixel newer than Postgres's for one cell → the rebuilt
  board has `ver`'s color there and Postgres's everywhere else
- **Single-flight:** 20 concurrent reads of a cold board run one Postgres scan
- **Board equals Postgres:** after a series of placements, the Redis board is byte-for-byte
  what `Canvases.Board` reads from Postgres
- **Cooldown:** five simultaneous placements → one placed, four refused, and the four did
  not reach Postgres; a placement whose transaction fails releases the key; after placing on a
  60-second canvas, a 10-second canvas in the same space is open again after 10 seconds
- **Tickets:** works once, refused the second time, expired after 30 seconds (Redis's clock,
  checked with a short TTL in the test)
- **Limits:** the same limits as today hold through Redis, and two limiters sharing one Redis
  share the count (the two-replica case)
- **Redis down** (a client pointed at a closed port): board read, placing, cooldown, login
  limit and tickets all still work, from Postgres and memory; nothing returns an error

## Order of work

1. **PR 1, Redis only:** `redis("Redis", …)` and `REDIS_URL` on `api`. Local plan, then the
   PR's plan comment: one database added, `api` variable added, nothing else, nothing public.
   **Owner reads the plan; merge.** The API ignores the variable until PR 2
2. **PR 2, the feature,** with the tests above. Shown locally first: the API on the laptop
   with Redis and Postgres in OrbStack, the board loading from Redis (`board_hits` climbing),
   then Redis stopped and the site still working (`redis_ok=0`, fallbacks in the log), then
   Redis started again and the board rebuilt
3. Merge. Check live: the stats line shows `redis_ok=1` and board hits; placing works; the
   Redis service's memory in Railway stays small (a 64 KB board, a few keys)
4. **Live "Redis down" drill, only with the owner's go-ahead:** stop the Redis service in
   Railway for a minute and check the site still loads and places pixels, then start it again.
   A dashboard action, the owner's to click
5. Notion: tick Step 11. STATUS updated

## Out of scope

Redis pub/sub between API copies (Step 14), the job queue (Step 12), caching anything besides
the board, Redis persistence settings (everything in it can be rebuilt), a Redis password
rotation (the URL is a Railway reference and rotates with the service).

## Risks

- **Railway's Redis template may add a public proxy or a volume.** The plan shows it before
  merging; a public proxy is removed, a volume is fine (and cheap)
- **The rebuild merge Lua script is the subtle part.** It gets the most tests, and the board
  expiry heals anything it misses within 10 minutes
- **More moving parts for one API copy.** True: with one copy, the in-memory limiter and
  Postgres tickets were enough. This step is about learning the pattern before Step 14 needs
  it, and the fallbacks keep the app no worse than today when Redis is gone
