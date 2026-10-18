# Task 017 — A k6 load test from the laptop against staging: what 100 players need, and what they cost

**Status: done 2026-10-18. 100 players pass with room to spare; they cost about \$0.15 an hour, almost
all of it egress (the fan-out). The full breakdown is in Notion, "Load test results (reference)".
200 and 500 players were considered and dropped by the owner (the laptop's own connection would
become the limit, and the question was answered).**

## Results (2026-10-18, 16:35–16:53 UTC)

k6 2.3.0 from the laptop (Indonesia) against staging (Singapore, two API copies). Each run: 1 min
ramp-up, 2 min held, 10 s down; a fresh canvas with a 2 s cooldown; every player a real session.

| Players | Requests/s | Pixels | HTTP p95 | HTTP p99 | Placed→seen p95 | Errors | API CPU avg (peak) | API memory | Egress | Run cost | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 10 | 4.5 | 526 | 75 ms | 96 ms | 71 ms | 0 | 0.01 vCPU | 19 MB | 5.6 MB | \$0.0003 | PASS |
| 25 | 11.0 | 1,259 | 122 ms | 210 ms | 133 ms | 0 | 0.03 vCPU | 26 MB | 15.3 MB | \$0.0008 | PASS |
| 50 | 22.5 | 2,602 | 119 ms | 183 ms | 127 ms | 0 | 0.05 vCPU | 33 MB | 39.9 MB | \$0.002 | PASS |
| 100 | 44.7 | 5,114 | 162 ms | 289 ms | 241 ms | 0 | 0.11 vCPU (0.20) | 38 MB (57) | 112.9 MB | \$0.006 | PASS |

Egress is what the laptop received (k6's `data_received`); Railway's per-minute estimate was about
half of it. The API's own (server-side) HTTP p95 stayed at 30–45 ms in every run: 32 ms at 100
players. The rest of the client-side latency is the internet and the laptop.

Every run: no dropped sockets (`ws_dropped_slow=0`), no Redis errors, the database pool never
saturated (`db_in_use` back to 0 between stats lines; `db_waited_total` rose by a handful over all
runs), both API copies shared the pixels (`bus_published` and `bus_received` about equal on each).
Postgres-staging peaked at 0.05 vCPU and 89 MB, Redis at 0.01 vCPU and 13 MB. The whole session's
Railway usage, staging included, went from \$0.4306 to \$0.4451 (`railway usage`): **all four runs
together cost under one cent.**

### What it means (the data point for the next project)

- **100 players at once: comfortable.** Server p95 32 ms; the API used 0.2 of 8 vCPU at its peak
- **Cost of 100 players for an hour ≈ \$0.15**: egress about 2.9 GB ≈ \$0.14, CPU about \$0.006.
  **Egress is ~95% of it**, and it grows with players × players: 200 ≈ \$0.6/h, 500 ≈ \$3.6/h,
  1,000 ≈ \$15/h (estimates)
- **A third of the egress was board downloads** (64 KB each, uncompressed); compressing the board is
  the cheapest saving. Then batching the broadcast, then rooms
- **Past 100, the first thing to give is the fan-out** (bandwidth and the egress bill), not the server

How the costs were measured: `railway metrics --since --until` per run (CPU and memory averages,
egress from k6's `data_received`, which Railway's per-minute average under-reports by about half), Railway's prices (vCPU \$20/month,
memory \$10/GB-month, egress \$0.05/GB), cross-checked against `railway usage` over the session.


Plan: [Rail Canvas, Step 17](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Load test" section (the 2026-10-04 draft: real-player flows, ramp slowly, pass or fail written down
before the run, a results table, raise the login limits in staging only; its "find the limit by
narrowing" is dropped, see the goal), "Cost" (egress grows with fan-out) and "Operations" (the stats line). Last stretch step
before the README (Step 18).

## The goal

The owner's question (2026-10-18): **can Rail Canvas handle 100 players at once, and what does
that cost?** Measured at 10, 25, 50 and 100 players, so the trend can be extrapolated later
("N players for an hour costs about this much, and the part that runs out first is X").

Not a goal: finding the absolute ceiling. Past 100 players is a later, deliberate choice.

**Why not the ceiling** (the owner's reasoning): on a fixed-price server (a VPS, the home server)
pushing until it breaks costs nothing extra and finds the box's limit. On Railway the bill follows
the load, and a bigger load is always available for more money, so "how much can it take" has no
useful answer. "What do 100 players use, and what does that cost" is a data point the next project
can price from.

## What the sanity check found (2026-10-18)

- **Railway's CLI reports usage per time window** (`railway metrics --since … --until …`: CPU,
  memory, network per service) and the month's bill (`railway usage`). So each run's cost is
  measured from the run's own window, not guessed from graphs, and not mixed with the rest of the day
- **No usage limit is set** on the account (`railway usage`: soft and hard limit "not set"; the
  month so far \$0.43, estimated \$0.74). STATUS and Notion believed there was a \$7 limit. So a
  runaway cannot stop production, but nothing caps the bill either (decision for the owner, below)
- **The cost to report is the extra the load adds**: memory is paid all month whether players come
  or not. Each run's cost = (CPU and egress in its window) − (the same window's baseline, measured
  in the minutes before it). The extrapolation is then "fixed monthly cost + cost per player-hour"
- **The fan-out estimate was off by a little**: 100 players placing every ~2.5 s is about 40 pixels
  a second, each sent to 100 sockets, about 4,000 messages a second (~0.6 MB/s), about 0.1 GB for a
  3-minute run. Fixed below
- **Logging 100 players in takes about 30 seconds** (bcrypt at cost 12 on the server, by design),
  longer than k6's default 60-second setup limit allows with margin; the script sets its own
- **k6's WebSocket API decides the script's shape**: a player's loop runs inside its socket's event
  loop (`k6/experimental/websockets` with timers), so placing and listening happen together

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
     at 100 players, about 40 pixels a second × 100 sockets ≈ 4,000 messages a second, ~0.6 MB/s. If
     the laptop or the Wi-Fi gives out first, that is a limit of the test, not of Rail Canvas; the
     run shows it (k6's own errors, dropped sockets on the laptop side while the API stays calm)
   - **All players share one IP**, which the login limit throttles: hence one paced login (decision 5)
3. **Only staging is ever hammered.** The script refuses any URL that is not staging's, like the
   staging tests do

### Staging for the test

4. **Staging runs two API copies during the test**, like production, so the test also covers the
   cross-copy WebSocket path (Step 14). `.railway/railway.ts`: staging's `api` replicas 2. It stays
   at 2 afterwards (about \$0.50 a month) so staging keeps matching production
5. **No limit is loosened** (changed in the last sanity check, 2026-10-18): sessions last 30 days, so
   `make loadtest-login` logs the players in **once**, paced under the per-IP login limit (20 at
   once, then one every 3 s: about 4 minutes for 100), and every run reuses those tokens from the
   git-ignored `ops/loadtest/.tokens.json`. No API code exists for the load test, so nothing in it
   can ever weaken production
6. **100 players are seeded**: `player-001@example.com` … `player-100@example.com`, members of
   `staging-club`, all with `SEED_OWNER_PASSWORD` (already in staging and in the laptop's `.env`).
   The seed gains `SEED_PLAYERS=100` and, like today, only adds what is missing, never in
   production. Same pre-deploy step, so a push creates them
7. **A canvas for the test, `load test`, with a 2-second cooldown**, made by the script at the
   start of each run (the staging tests already make their own canvases). Two seconds keeps players
   placing often enough to load the system; ten would make most of a run idle waiting

### Logging in

- **No bypass.** Every player logs in for real with its own email and password and gets its own
  session token (an opaque bearer token, checked live against the database on every request, not a
  JWT). That check is part of every request in production, so it stays in the test; a bypass would
  measure a different system
- Logins happen once per player, before any run (decision 5), not on the clock

### What one player does (the k6 script)

8. **Like a person, not a hammer** (Notion's draft), random cells and colours:
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

11. **Four runs: 10, 25, 50, 100 players**, each 3 minutes (1 minute ramp-up, 2 held), one command
    each: `make loadtest players=50`. About 12 minutes of load in all. If a run fails, the next
    ones are skipped: that level is the answer. No narrowing runs
12. **While a run goes, I read staging**: the stats lines of both copies (`ws_clients`,
    `ws_dropped_slow`, `db_in_use`, `db_waited_total`, `bus_published`, `bus_received`,
    `redis_errors`), and Railway's CPU, memory and network graphs for `api`, `Postgres-staging`,
    `Redis-staging`. One row per run in the results table
13. **What each run cost**, for the extrapolation: `railway metrics --since <start> --until <end>`
    for `api`, `Postgres-staging`, `Redis-staging` (CPU, memory, egress) × Railway's prices (vCPU
    \$20 per month, memory \$10 per GB-month, egress \$0.05 per GB), minus the same services'
    baseline in the 3 minutes before. Cross-checked against `railway usage` before and after the
    session. The table gets "extra cost per run" and "cost per player-hour"

### The cost cap

- **The whole step stays far under \$5**: four short runs, about 12 minutes of load. Estimated egress
  for the 100-player run ≈ 0.1 GB (\$0.005); CPU and memory a few cents. **The script refuses more
  than 100 players**, so a typo cannot start a big run
- **No usage limit is set today** (sanity check above). Recommended before the first run: a hard
  limit, for example \$10, so no mistake can run up a bill. Reaching a hard limit stops every service,
  production included, which at \$10 against a \$0.74 month is far away

### Results

14. **The results table lives in this file and in Notion's Step 17**: players, requests a second,
    p95, p99, errors, placed-to-seen p95, API CPU and memory per copy, Postgres connections in use,
    egress, cost of the run, pass or fail. Then the extrapolation: cost per player-hour, and which
    measure grows fastest (the first to run out past 100). Step 18's README quotes it

## Cost

A run of 100 players for 3 minutes: the fan-out is about 0.6 MB/s, about 0.1 GB of egress, about
\$0.005. CPU while running: a few cents. All runs together: well under \$1. The second API copy on
staging: about \$0.50 a month.

## Files

```
ops/loadtest/script.js          # the k6 player: the visit, the loop, "placed to seen", the summary
ops/loadtest/login.sh           # logs the players in once, paced; writes .tokens.json
ops/loadtest/README.md          # how to run it, what each line of the summary means
Makefile                        # make loadtest-login, make loadtest players=N (k6 2.3.0 in Docker)
api/internal/service/seed.go    # SeedPlayers
api/cmd/cli/seed.go             # SEED_PLAYERS
.railway/railway.ts             # staging: api replicas 2, SEED_PLAYERS=100
CLAUDE.md (a Load test section), .env.example
```

## Tests

- **The seed:** `SEED_PLAYERS=3` on a fresh database adds three players who can log in and are
  members; run again, it adds nothing; in production, nothing
- **The script, at 3 players against a local API** before any run on staging: it logs in, places,
  measures "placed to seen", and refuses production's URL
- Every existing test still passes

## Order of work

1. Build and test locally (the switch, the seed, the script at 3 players against a local API)
2. Show the owner. Then, with the owner's OK, merge into `staging` and push: staging gets two API
   copies, the switch and 100 players; the staging tests run as usual
3. **The runs, together with the owner** (about 20 minutes): 10, 25, 50, 100 players. I read the
   stats and the graphs during each run and fill in the table, with each run's cost
4. Results into this file and Notion; release pull request `staging` → `main` for the code
   (production gets the switch's refusal and nothing else: no load test ever touches it)
5. Notion: tick Step 17. STATUS updated

## Out of scope

Testing production (never), more than 100 players, a no-wait "hammer" run, a distributed test from several machines or regions, testing the
template upload or timelapse paths under load (they are rare actions), autoscaling, changing code to
pass a threshold (a fix would be its own step, after the result is in).

## Risks

- **The laptop is the bottleneck before Rail Canvas.** Seen as k6-side errors while staging's stats
  stay calm; the run is then reported as "limit of the test", and the bigger runs are skipped
- **No cap on the bill** until the owner sets a usage limit (decision below). These runs cost
  cents; the script's 100-player cap and fixed 3-minute runs are the guard until then
- **A run left going** costs egress: every run has a fixed 3-minute duration, and Ctrl-C stops it
- **Staging's history grows** by a few hundred thousand pixels; staging's nightly cleanup and the
  30-day retention take care of it, and staging's data is fake

## Decided with the owner (2026-10-18)

- Thresholds: mine for latency; **errors must be zero**
- Still open: **a hard usage limit** (my suggestion \$10), set by the owner in Railway → Workspace
  → Usage, or by me with `railway usage limit set` on the owner's OK
- Logins are real, one token per player
- The question is 100 players and what they cost, measured at 10, 25, 50, 100; no ceiling hunt, no
  hammer run (a later step if wanted). The script refuses more than 100 players
