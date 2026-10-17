# Task 014 — Two API replicas, with WebSocket messages passed between them through Redis pub/sub

**Status: approved 2026-10-17 after a sanity check (a copy that failed to announce now asks the others to resync; the listener pings to find a dead connection; Railway's replica id; bus counters on the stats line). Built, shown locally, pull request open.**

## Local run (2026-10-17)

Two API copies on the laptop (ports 8091 and 8092), one Redis 8.2 and Postgres 18 in OrbStack, a
socket on each copy, pixels placed through copy 1:

| Moment | Socket on copy 1 | Socket on copy 2 |
| --- | --- | --- |
| Redis up, pixel 1 | got it once | got it once, 0.0 s later |
| Redis stopped, pixel 2 | got it | nothing (as expected) |
| Redis started (+0.9 s) | `resync` (its listener reconnected) | `resync` (its listener reconnected) |
| +22 s | | `resync` for the canvas (copy 1's missed announcement made good) |

Stats lines: copy 1 `bus_published=2 bus_resyncs=1`, copy 2 `bus_received=1 bus_resyncs=1`.
Demo database dropped, Redis flushed, the throwaway program deleted.

## What changed while building

- **`nothingMore` in the tests must come last**: a read with a deadline closes a coder/websocket
  connection (the Step 6 gotcha), so the "nothing arrived" checks use the bus counters mid-test
- **The test that matters was proven to catch its bug**: removing the "skip my own message" check
  makes `TestBusTwoCopiesDeliverOnce` fail with a pixel seen twice

Plan: [Rail Canvas, Step 14](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Realtime" (Across replicas: each replica publishes and subscribes, messages carry the sender's
id), "Data model" (pub/sub channel `space:{s}:canvas:{c}`) and "Cost" (a second replica, about
$0.50 a month). Fourth stretch step.

## The goal

Today one copy of the API runs. Every open WebSocket is connected to it, so when someone places a
pixel, the API sends it to every socket it holds and everyone sees it.

With **two copies** (Railway "replicas"), Railway spreads the sockets between them. Player A on
copy 1 places a pixel; copy 1 tells its own sockets, but player B on copy 2 hears nothing, because
copy 2 never saw the placement. The board is still right (Postgres has it; a reload shows it), but
"live" breaks for half the players.

The fix: each copy also **announces** every placement on Redis, and **listens** to Redis for the
other copies' announcements, passing them on to its own sockets. Redis pub/sub is a radio: anyone
can broadcast on a channel, everyone tuned in hears it, nothing is stored.

## Why this exists (and whether we need it)

Real services run several identical copies for three reasons: **surviving a crash** (one copy
dies, the other keeps serving while it restarts), **growing past one machine** (a container can
only get so big), and **spreading load** (no single copy holds every socket). Grab or Stockbit run
every API this way, adding and removing copies as traffic changes.

Rail Canvas does not need it: one Go process uses all its cores and handles thousands of
requests a second. It is here for the lesson every backend learns once: **with two copies,
anything kept in one process's memory stops being shared.** Steps 11 (rate limits and tickets in
Redis) and 13 (no timer in the API) prepared for it; this step fixes the last piece, the
WebSocket hub, and proves it live.

## Words used below

- **Replica**: one running copy of the same service. Railway starts two containers from the same
  image and splits incoming connections between them
- **Pub/sub**: Redis's `PUBLISH channel message` sends to everyone `SUBSCRIBE`d (or
  `PSUBSCRIBE`d, by pattern) at that moment. A copy that is not listening misses it; nothing is
  kept, unlike the job queue
- **Fan-out**: one placement turned into one message per connected player

## Decisions for this step

### Passing messages between copies

1. **Local first, then Redis.** `Publish` still hands the message to this copy's own sockets
   at once (as today), then `PUBLISH`es it to Redis on `space:{s}:canvas:{c}` (the Notion name),
   wrapped as `{"from": <this copy's id>, "topic": …, "msg": …}`
2. **Each copy listens to every canvas** with one `PSUBSCRIBE space:*:canvas:*`, and passes on
   what arrives to its own sockets, **skipping messages whose `from` is itself** (they were
   delivered locally already: no player sees a pixel twice). Listening to every canvas instead of
   subscribing per canvas as players come and go: far simpler, and at this size every copy has
   players on the one canvas anyway
3. **The copy's id** is Railway's `RAILWAY_REPLICA_ID` (the same id Railway's log view filters
   on), or a random one on the laptop. It goes on every
   log line as `replica`, so the stats lines of the two copies can be told apart
4. **A separate Redis connection for listening**, with Redis's default timeouts. The API's client
   has 200 ms read timeouts on purpose (Step 11); a listener waits for minutes between messages and
   would time out constantly on it. Publishing uses the API's client and its circuit breaker
5. **The space goes into `Publish`**: `Publish(spaceID, canvasID, msg)`, since the channel name
   carries it (everything outside Postgres carries the space). The topic for sockets stays
   `canvas:{id}`

### When Redis is down or the listener drops

6. **A placement still reaches the players on its own copy**; the other copy's players miss it.
   Nothing returns an error (the Step 11 rule)
7. **After the listener reconnects, its copy tells its players to reload the board**: a new
   server message `resync`. The frontend already reloads on reconnect; it does the same on
   `resync`. So a gap costs one board reload, never a board that stays wrong. go-redis reconnects
   by itself; a second "subscribed" confirmation means it did
8. **Publishing while the breaker is open is skipped**, like every other Redis use: one timeout
   per outage, not one per pixel
9. **A copy that could not announce a pixel asks for a resync once Redis answers again.** The
   listeners of the other copies may never have dropped (only this copy's call failed), so (7)
   alone would miss it. The copy remembers which canvases missed an announcement and, at its next
   good Redis check (every 30 s, as for stale boards), publishes `resync` for each; the other
   copies pass it to that canvas's players
10. **A silent dead connection is found:** the listener waits at most a minute for a message, then
    pings Redis; no answer means reconnect (and then resync)

### Running two copies

11. **`replicas: 2`** for `api` in `.railway/railway.ts`. Railway spreads connections between them.
   A WebSocket stays on the copy it opened on
12. **Everything else already works across copies**, checked one by one:

    | Shared state | Across two copies |
    | --- | --- |
    | Sessions, cooldown, board | Postgres (truth) and Redis, both shared |
    | WebSocket tickets | Redis (or `ws_tickets`), so a ticket from copy 1 opens a socket on copy 2 |
    | Rate limits | Redis, shared. During a Redis outage each copy counts alone (twice the limit, briefly) |
    | Board rebuild | `singleflight` per copy plus the `SET NX` lock across copies |
    | Hourly snapshot | the cron service since Step 13; no timer in the API |
    | Migrations | the pre-deploy command, once per deploy, not per copy |
    | Postgres connections | 2 × 10 (API) + 4 (worker) + 2 + 2 (cron) = 28, far under 500 |

13. **The stats line adds `bus_published`, `bus_received`, `bus_resyncs`**: the live check reads
    them per copy
14. **A deploy replaces both copies**; each says "going away" to its sockets, which reconnect to
    whichever copy is up. Same as today, twice

## API and frontend

- `openapi.yaml` 0.6.0: `WSServerMessage.type` gains `resync`
- `useLiveCanvas`: on `resync`, reload the board (the code path a reconnect already uses)

## Files

```
api/internal/realtime/bus.go      # Bus: local publish + Redis PUBLISH; the listener; resync
api/internal/realtime/hub.go      # Resync(): tell every local client to reload
api/internal/service/pixels.go    # Publisher takes the space
api/cmd/api/main.go               # replica id on the logger; the listener's own Redis client
api/openapi.yaml, web/src/lib/hooks/useLiveCanvas.ts
.railway/railway.ts               # api: 2 replicas
CLAUDE.md (Realtime), .env.example
```

## Tests

- **Two copies, real Redis:** two hubs with their own buses on one Redis. A socket on copy B
  receives a pixel published on copy A, **exactly once**; a socket on copy A also receives it
  exactly once (its own message from Redis is skipped)
- **Redis down:** the publishing copy's sockets still get the pixel, and nothing errors
- **Listener reconnects:** the copy's sockets get `resync`
- **A failed announcement:** after Redis comes back, the other copy's sockets on that canvas get
  `resync`, and only that canvas's
- **Space in the channel:** a pixel in space S goes out on `space:S:canvas:C`
- Every existing test still passes (the hub's tests use the local path, unchanged)

## Order of work

1. Build and test locally. Show it: **two API copies on the laptop** (ports 8091 and 8092), one
   Redis, Postgres; a socket on each; a pixel placed through copy 1 appears on both sockets once.
   Stop Redis: the pixel reaches copy 1's socket only. Start Redis: copy 2's socket gets `resync`
2. **One pull request**: code, frontend and `replicas: 2`. **Owner reads the plan; merge**
3. Check live: two stats lines every 30 s, one per `replica`. Then with the owner: a few
   WebSockets opened from my side (as the owner's account, listening only, no pixels placed), the
   owner places one pixel in the browser, and **every socket receives it once**, whichever copy it
   landed on; the stats lines show sockets on both copies. Then the owner tries two browsers
   (or a phone and the laptop), as Notion asks
4. Notion: tick Step 14. STATUS updated

## Out of scope

Subscribing per canvas (only worth it with many canvases), more than two copies, sticky sessions
(not needed: every copy serves every request), a replayable stream (Redis Streams) instead of
pub/sub (`resync` covers a gap), moving the worker to two copies.

## Risks

- **Cost:** a second API copy, about 30–50 MB, roughly $0.30–0.50 a month
- **A board marked stale on one copy** (Step 11: a placement that missed the Redis board while
  the breaker was open) is dropped from Redis when that copy's Redis comes back, within 30 s. The
  other copy may serve that board for those seconds. Same window as today, now seen from two copies
- **Load balancing is Railway's**: with few sockets, all may land on one copy by chance. The live
  check opens several to see both copies used
