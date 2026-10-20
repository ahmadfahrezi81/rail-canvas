# Task 018 — A README with the architecture, the real numbers and what was learned

**Status: approved 2026-10-20 ("go ahead, finish up step 18"). Docs only: no code, no Railway
change.**

Plan: [Rail Canvas, Step 18](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9): "A short
README with an architecture diagram, the final Usage page numbers and notes on what was learned:
what broke, and what cost more than expected." The last step of the project.

## What goes in `README.md`

Short, for someone landing on the GitHub repo (the owner in a year included). The detail stays where
it already lives: `CLAUDE.md` (how the code is built), `docs/runbook.md` (operations), Notion (every
decision and the full load test). The README links to them, it does not copy them.

1. **What it is**, in two lines, with the live site and API
2. **Architecture**: a Mermaid diagram (GitHub renders it, no image to keep in sync): Cloudflare
   Pages, the API's two copies, Postgres, Redis, the bucket, the worker, the three cron services,
   the bot Function, R2 and healthchecks.io outside Railway. Then one line per piece saying its job
3. **How a change ships**: local branch, `staging`, CI, staging tests, release PR into `main`
4. **Running it locally**: the few `make` targets, pointing at `CLAUDE.md` for the rest
5. **The numbers**: the bill from `railway usage` (the Usage page's numbers, from the CLI) for the
   project's whole life so far (2026-10-04 to 2026-10-20) per service, the monthly estimate, and
   the load test's headline (100 players, latency, cost per hour)
6. **What was learned**: what broke and what cost more (or less) than expected, the short version
   of Notion's "Lessons learned (Railway)"
7. **Layout**: the top-level folders in one table

## Numbers (2026-10-20, `railway usage --json`, `railway usage projects`)

Billing period 2026-10-04 to 2026-11-04, so far: **\$0.62** (memory \$0.55, CPU \$0.02, egress
\$0.02, volume \$0.02); Railway's estimate for the month **\$1.40**. Per service: Postgres \$0.35,
api \$0.14, Postgres-staging \$0.03, bot \$0.03, worker \$0.03, Redis \$0.02, Redis-staging \$0.01.
The cron services and the buckets do not appear in the per-service list. Staging only exists since
2026-10-18.

## Also

- Notion: a **Conclusion** section (the owner's offer), Step 18 ticked once the README is on `main`,
  the callout and the Cost section's monthly figure brought up to date
- `STATUS.md`: Step 18 done; the project is finished, what is left is optional

## Not in this step

No badges, no screenshots, no licence (a personal learning project; the owner can add one).
