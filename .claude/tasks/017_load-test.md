# Task 017 — A k6 load test from the laptop against staging, to find where Rail Canvas bends

**Status: draft 2026-10-18, waiting for the owner's review.**

Plan: [Rail Canvas, Step 17](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Load test" section (the 2026-10-04 draft: real-player flows, ramp slowly, pass or fail written down
before the run, find the limit by narrowing, a results table, raise the login limits in staging
only), "Cost" (egress grows with fan-out) and "Operations" (the stats line). Last stretch step
before the README (Step 18).

## The goal

Find out **how many players Rail Canvas carries on Railway before it gets slow or starts failing**,
and **which part gives out first**: the API's CPU, its memory, Postgres connections, Redis, the
WebSocket fan-out, or the network. Write the answer down as a table of runs.

Not a goal: a big number. "It bends at 60 players, and the reason is X" is a full result.

## Why this exists

Every real service gets load tested before a launch or a sale, because the failure you meet under
load is never the one you guessed: a pool of 10 database connections, a lock, a chatty WebSocket.
Tokopedia and Shopee rehearse their big sale days this way. The lessons here:

- **Measure, then guess.** The stats line and Railway's graphs say what ran out
- **Percentiles, not averages.** p95 is "19 of 20 requests are at least this fast"; p99 is the
  slow tail users actually complain about. An average hides both
- **A threshold is decided before the run**, or every result looks fine afterwards

## Words used below

- **k6**: a load-testing tool. A JavaScript file describes what one player does; k6 runs that file
  as many players at once (**virtual users**, VUs) and measures every request
- **Ramp**: start with few players and add more in steps, holding each step a few minutes
- **Fan-out**: one placed pixel is sent to every connected player. With N players each placing,
  messages grow with N × N: the cost that grows fastest here

## Decisions for this step

### Where k6 runs

1. **From the laptop** (the owner's call, 2026-10-18), not from its own Railway project as Notion
   planned: nothing to deploy, nothing to delete afterwards. k6 runs in Docker
   (`grafana/k6`, a pinned version), so nothing is installed
2. **What that changes**, written down so the results are read right:
   - Every request crosses the owner's internet to Singapore (about 20–40 ms each way from
     Indonesia). That is what real players see too, so it stays in the numbers; the API's own time
     is on the stats line and in Railway's metrics, which do not include it
   - **The laptop's connection is part of the test.** The fan-out comes back down to the laptop:
     at 100 players, roughly 100 × 50 pixels a second ≈ 5,000 messages a second, under 1 MB/s. If
     the laptop or the Wi-Fi gives out first, that is a limit of the test, not of Rail Canvas; the
     run shows it (k6's own errors, dropped sockets on the laptop side while the API stays calm)
   - **All players share one IP**, which the login and signup limits would throttle (decision 5)
3. **Only staging is ever hammered.** The script refuses any URL that is not staging's, like the
   staging tests do

### Staging for the test

4. **Staging runs two API copies during the test**, like production, so the test also covers the
   cross-copy WebSocket path (Step 14). `.railway/railway.ts`: staging's `api` replicas 2. It stays
   at 2 afterwards (about \$0.50 a month) so staging keeps matching production
5. **The per-IP limits get a staging-only switch.** A new variable `RATE_LIMITS=relaxed` multiplies
   the login, signup and join limits by 100. **The API refuses to start with it in production**
   (checked at boot from `RAILWAY_ENVIRONMENT_NAME`), so it can never weaken production by
   accident. Set on staging's `api` in the file, so it is visible and reviewed
6. **200 players are seeded**: `player-001@example.com` … `player-200@example.com`, members of
   `staging-club`, all with `SEED_OWNER_PASSWORD` (already in staging and in the laptop's `.env`).
   The seed gains `SEED_PLAYERS=200` and, like today, only adds what is missing, never in
   production. Same pre-deploy step, so a push creates them
7. **A canvas for the test, `load test`, with a 2-second cooldown**, made by the script at the
   start of each run (the staging tests already make their own canvases). Two seconds keeps players
   placing often enough to load the system; ten would make most of a run idle waiting

### Logging in

- **No bypass.** Every player logs in for real with its own email and password and gets its own
  session token (an opaque bearer token, checked live against the database on every request, not a
  JWT). That check is part of every request in production, so it stays in the test; a bypass would
  measure a different system
- Logins happen once per player, in k6's setup step, before the clock starts

### What one player does (the k6 script)

8. **Like a person, not a hammer** (Notion's draft):
   - log in once (k6's setup step logs every player in before the clock starts, so login cost
     is measured on its own and does not pollute the placing numbers)
   - load `/me`, the canvas list and the board; get a WebSocket ticket, connect, subscribe
   - then loop: wait 1–3 seconds (random), place a pixel at a random cell in a random colour; about
     one loop in 20 reloads the board instead (as a page refresh would)
   - a refused placement for the cooldown (429) counts as normal, not as an error: it is the game's
     rule working
9. **One custom measure: "placed to seen"**, the time from a player's `POST …/pixels` to the same
   pixel arriving on that player's own socket. It is the whole realtime path: Postgres, Redis
   pub/sub between the copies, the hub, the network. The number players feel

### The runs

10. **Pass or fail, decided now** (thresholds for latency are mine, the owner's call; errors: zero):

    | Measure | Pass |
    | --- | --- |
    | HTTP p95 (all requests except login) | under 300 ms |
    | HTTP p99 | under 800 ms |
    | Errors (anything but 2xx and the cooldown 429) | **0** (the owner's call: the first run with any error marks the level) |
    | "Placed to seen" p95 | under 500 ms |
    | Sockets dropped by the server (`ws_dropped_slow`) | 0 |

11. **Steps: 10, 25, 50, 100, 200 players**, each a separate 3-minute run (1 minute ramp-up, 2
    minutes held), one command each: `make loadtest players=50`. The first step that fails marks
    the ceiling; then two or three runs in between narrow it (Notion: a binary search, stopped once
    the gap is small). At most about 10 runs, about 30 minutes in all
12. **While a run goes, I read staging**: the stats lines of both copies (`ws_clients`,
    `ws_dropped_slow`, `db_in_use`, `db_waited_total`, `bus_published`, `bus_received`,
    `redis_errors`), and Railway's CPU, memory and network graphs for `api`, `Postgres-staging`,
    `Redis-staging`. One row per run in the results table
13. **If 200 players still pass**, the next step is not more players from the laptop (its own
    connection would become the limit) but a shorter cooldown (1 second), which doubles the load per
    player. Noted as a result, and the run stops there

### Two kinds of run

- **Players** (decisions 8–13): realistic people, random cells, waits between actions. Answers
  "how many players at once?"
- **Hammer**, after the player runs: no waits. Logged-in players fire requests back to back, and k6
  raises the rate step by step (an arrival rate: 50, 100, 200, 400 … requests a second) until the
  first error or p99 breaks. The mix: reading the board (the Redis cache), placing pixels (most get
  the cooldown 429, which is the cheap early refusal, and is not an error), `/me`. Answers "how many
  requests a second can the API take?" Same thresholds, 2 minutes per rate

### The cost cap

- **Budget for all of Step 17: under \$5** (the owner's call). Railway bills CPU by the minute
  (about \$0.0005 per vCPU-minute) and egress at \$0.05 per GB. The cost that grows fastest is the
  fan-out: with N players placing every ~2.5 s, about N × N / 2.5 messages a second go out
- **Estimates per 3-minute run:** 100 players ≈ 0.1 GB, 200 ≈ 0.4 GB, 500 ≈ 2.5 GB (≈ \$0.13).
  CPU stays at cents. So the money cap is far away; **the hard caps are 500 players and 2,000
  requests a second**, written into the script (it refuses more), because past those the laptop's
  own connection is the limit, not Rail Canvas
- **Railway's usage limit (\$7) still stops production too** if the month's total reaches it. Before
  the first run the owner looks at the Usage page; if the month is already near \$5, raise the limit
  for the day

### Results

14. **The results table lives in this file and in Notion's Step 17**: players, p95, p99, errors,
    placed-to-seen p95, API CPU and memory per copy, Postgres connections in use, egress, pass or
    fail, and one line on what gave out. Step 18's README quotes the conclusion

## Cost

A run of 100 players for 3 minutes: the fan-out is under 1 MB/s, about 0.2 GB of egress, about
\$0.01. CPU while running: a few cents. All runs together: well under \$1. The second API copy on
staging: about \$0.50 a month.

## Files

```
ops/loadtest/script.js          # the k6 player: login in setup, the loop, "placed to seen"
ops/loadtest/README.md          # how to run it, what each measure means
Makefile                        # make loadtest players=N (k6 in Docker, reads the laptop's .env)
api/internal/config/config.go   # RATE_LIMITS=relaxed, refused in production
api/internal/handler/server.go  # the limits read the switch
api/internal/service/seed.go    # SEED_PLAYERS
api/cmd/cli/seed.go             # passes it on
.railway/railway.ts             # staging: api replicas 2, RATE_LIMITS, SEED_PLAYERS
CLAUDE.md (a Load test section), .env.example
```

## Tests

- **The switch:** `RATE_LIMITS=relaxed` with `RAILWAY_ENVIRONMENT_NAME=production` stops the API at
  boot; elsewhere the limits are 100 times looser; unset, they are as today
- **The seed:** `SEED_PLAYERS=3` on a fresh database adds three players who can log in and are
  members; run again, it adds nothing; in production, nothing
- **The script, at 3 players against a local API** before any run on staging: it logs in, places,
  measures "placed to seen", and refuses production's URL
- Every existing test still passes

## Order of work

1. Build and test locally (the switch, the seed, the script at 3 players against a local API)
2. Show the owner. Then, with the owner's OK, merge into `staging` and push: staging gets two API
   copies, the switch and 200 players; the staging tests run as usual
3. **The runs, together with the owner** (about 30 minutes): 10, 25, 50, 100, 200 players, then the
   narrowing. I read the stats and the graphs during each run and fill in the table
4. Results into this file and Notion; release pull request `staging` → `main` for the code
   (production gets the switch's refusal and nothing else: no load test ever touches it)
5. Notion: tick Step 17. STATUS updated

## Out of scope

Testing production (never), a distributed test from several machines or regions, testing the
template upload or timelapse paths under load (they are rare actions), autoscaling, changing code to
pass a threshold (a fix would be its own step, after the result is in).

## Risks

- **The laptop is the bottleneck before Rail Canvas.** Seen as k6-side errors while staging's stats
  stay calm; the run is then reported as "limit of the test" and decision 13 applies
- **Railway's usage limit** (\$7) stops every service, production included, if reached. These runs
  cost cents, but the owner glances at the Usage page before the first run
- **A run left going** costs egress: every run has a fixed 3-minute duration, and Ctrl-C stops it
- **Staging's history grows** by a few hundred thousand pixels; staging's nightly cleanup and the
  30-day retention take care of it, and staging's data is fake

## Decided with the owner (2026-10-18)

- Thresholds: mine for latency; **errors must be zero**
- Logins are real, one token per player
- Under \$5 for the whole step; hard caps 500 players and 2,000 requests a second
- Both kinds of run: players, then hammer
