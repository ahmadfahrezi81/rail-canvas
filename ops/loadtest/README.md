# Load test (Step 17)

k6 from the laptop, against **staging only**: what 100 players need, and what they cost. Plan and
results: `.claude/tasks/017_load-test.md`.

```bash
make loadtest-login          # once: logs player-001 … player-100 in (about 4 min); tokens last 30 days
make loadtest players=10     # one run: 1 min ramp-up, 2 min held, 10 s ramp-down
make loadtest players=25
make loadtest players=50
make loadtest players=100    # the cap; the script refuses more
```

Needs Docker and `SEED_OWNER_PASSWORD` in the root `.env` (the seeded players' password).
`STAGING_URL` defaults to staging's API. A quick local try against `make dev`-style API:
`RAMP=10s HOLD=40s VISIT_MS=20000 make loadtest players=3 STAGING_URL=http://host.docker.internal:8080`.

## What one player does

Loads `/me`, the canvas list and the board, opens a WebSocket with a ticket, subscribes, then every
1–3 s places a pixel at a random cell (about one time in 20 it reloads the board instead). After a
minute it leaves and comes back with a new socket. Every player has its own real session token.

## What the summary means

| Line | Meaning |
| --- | --- |
| requests | every HTTP request, and how many a second |
| errors | anything but 2xx and the cooldown's 429; the threshold is zero |
| http p95 / p99 | 19 in 20 / 99 in 100 requests were at least this fast |
| placed to seen p95 | from placing a pixel to seeing it on your own socket: the realtime path players feel |
| result | PASS when every threshold held |

The raw numbers of each run land in `results/` (not committed).
