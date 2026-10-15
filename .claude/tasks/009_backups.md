# Task 009 — Nightly backups: `pg_dump` to Cloudflare R2, a dead-man's switch, one real restore

**Status: live 2026-10-15 (PR #8 the backup, #9 Wait for CI, #10 the runbook). First production backup `postgres/2026/10/15/160357-railway.dump` (181,101 bytes, 5 s), restored on the laptop: row counts match on all 11 tables, and `app` with `pixel-club` as the tenant sees its canvas, pixels and template (none without a tenant). healthchecks.io green from Railway's address.**

## After the merge

- Railway did not keep `checkSuites` or `builder: "DOCKERFILE"` when it created the service (the next plan showed both). `builder` dropped (Railway finds the Dockerfile in `rootDirectory`); `checkSuites` applied by PR #9, after which the plan is clean
- **Watch paths work:** the deploy for PR #9 (no change under `ops/backup/`) was `SKIPPED`
- The owner first pasted a wrong `HC_PING_URL`. The script logged no ping failure, because the wrong address still answered OK: only the check's own event list proves the right check gets the pings
- `ws_tickets` holds 2,544 used tickets: nothing deletes them yet. Step 13's nightly cleanup

## Spike results (2026-10-15, laptop, OrbStack)

- **Railway's Backups tab is Pro only** (Hobby shows it with an upgrade notice). Decision 11: no volume snapshots; the R2 dump is the only backup
- Owner made: R2 bucket `rail-canvas-backups` (APAC hint, public access off, delete after 30 days), an Account API token scoped to that bucket (Object Read & Write), a healthchecks.io check (cron `0 19 * * *` UTC, grace 1 hour, email)
- Against a throwaway `postgres:18` with every migration and 5,000 pixels: dump 61,670 bytes, 11 tables counted, **uploaded with plain `curl --aws-sigv4`** (no `rclone` needed), healthchecks.io green
- `restore.sh latest` into a second empty `postgres:18`: found the newest key, created `app`, restored, **row counts match** on all 11 tables
- A wrong password: `backup failed`, step `start`, healthchecks.io red with `step=start exit=2` in the event
- Two bugs found and fixed: the ERR trap did not run for errors inside functions (`set -E`), and a failure inside `$(…)` was reported twice (`on_error` now reports only from the main shell)
- `railway config plan`: `1 to add, 0 to change, 0 to destroy` (`Create service backup`). The plan does not print the service's settings, so the cron schedule and watch paths are checked in the dashboard after the apply

Plan: [Rail Canvas, Step 9](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Operations" and "Open questions" (Railway backups) sections.
Docs: [IaC reference](https://docs.railway.com/infrastructure-as-code/reference),
[Railway cron jobs](https://docs.railway.com/reference/cron-jobs),
[Railway backups](https://docs.railway.com/reference/backups),
[Cloudflare R2](https://developers.cloudflare.com/r2/),
[healthchecks.io](https://healthchecks.io/docs/).

## The goal

Today the database has no backup at all: if the Postgres volume is lost, or a
bad migration deletes data, it is gone. After this step:

- every night a small Railway service dumps the database and uploads the file
  to **Cloudflare R2**, a different company from Railway
- if a night's backup fails or does not run, the owner gets an **email**
- one backup has been **restored for real** and checked, so we know the files
  work
- `docs/runbook.md` says how to restore, roll back and replace a leaked secret

## Words used below

- **`pg_dump`**: Postgres's own tool that writes a whole database into one
  file. `pg_restore` loads that file into an empty database
- **Cron service**: a Railway service that is not always on. Railway starts it
  on a schedule (like an alarm clock), it does its job and exits, and it is
  billed only for the minute it runs
- **Dead-man's switch**: a check that expects a "still alive" ping every day.
  The backup pings it when it succeeds; if no ping arrives in time, the check
  emails you. It catches the failure nobody sees: a backup that silently
  stopped running
- **R2**: Cloudflare's file storage, S3-compatible like Railway's bucket. Free
  up to 10 GB, which is far more than we need

## Decisions for this step

1. **A separate image, `ops/backup/Dockerfile`, `FROM postgres:18`.** The API
   image has no `pg_dump`, and a `pg_dump` older than the server (18.6) refuses
   to dump it. The image adds `curl` and two shell scripts, `backup.sh` and
   `restore.sh`. No Go: the job is "run one tool, upload one file"
2. **A cron service `backup`, declared in `.railway/railway.ts`.** The SDK
   (3.16.0) has every setting it needs: `deploy.cronSchedule`,
   `deploy.restartPolicyType: "NEVER"` (a failed run reports and stops, it does
   not loop), `rootDirectory: "ops/backup"`, and `watchPatterns` so pushes that
   do not touch `ops/backup/` do not rebuild it. Singapore, beside Postgres.
   **Schedule `0 19 * * *`**: Railway's clock is UTC, so 03:00 in Singapore,
   the quietest hour. `checkSuites: true` like `api`, so a red `main` is not
   deployed. Whether `watchPatterns` are relative to the repo root or to
   `rootDirectory` is not documented; the plan shows it, and the first push
   that touches only `api/` must not rebuild `backup`
3. **It connects as `postgres`**, through `DATABASE_URL: Postgres.env.DATABASE_URL`
   (a reference, over the private network). Not as `app`: RLS would hide every
   row of the space-owned tables from `app`, and the dump would be silently
   empty. This is the one place outside migrations that uses `postgres`
4. **What one run does, in order** (`backup.sh`, `set -euo pipefail`):
   1. ping healthchecks.io `/start` (so it can also tell "slow" from "never ran")
   2. `pg_dump --format=custom` to a file in `/tmp` (compressed; lets
      `pg_restore` pick single tables later)
   3. `pg_restore --list` on that file: proves it is a readable archive (with
      `pg_dump`'s exit code and the size in the log, the best check short of
      a restore, which is what step 4 of the order of work is for)
   4. row counts of every table, written to a small `counts.txt`, for the
      restore test to compare against. Counted right after the dump, not
      inside its snapshot, so a pixel placed in those few seconds makes
      `pixels` differ by one; at 03:00 that is normally nothing
   5. upload both to R2 with `curl --aws-sigv4` (curl signs the request the
      way S3 expects; no AWS tools in the image). Keys:
      `postgres/YYYY/MM/DD/railway-HHMMSS.dump` and `…counts.txt`
   6. ping healthchecks.io (success). On any error, ping `/fail` with the
      error line, so the email says what broke. One log line per step, with
      the dump's size in bytes
5. **Kept for 30 days**, by an R2 lifecycle rule (one click in Cloudflare),
   not by our script. A script that deletes old backups is a script that can
   delete all backups
6. **The R2 key can only touch this one bucket** (an R2 API token scoped to
   `rail-canvas-backups`, "Object Read & Write"). If it leaks, nothing else in
   the Cloudflare account is exposed
7. **Variables on `backup`:** `DATABASE_URL` (reference), and five with
   `preserve()` whose values the owner types into Railway once:
   `R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`,
   `HC_PING_URL`. Never in git, never in chat. All listed in `.env.example`
8. **The restore test runs on the laptop, in OrbStack** (installed, not
   running today): a throwaway `postgres:18` container, the newest dump
   downloaded from R2, `restore.sh` creates the `app` role first (`pg_dump`
   leaves roles out), restores, compares row counts with `counts.txt`, then
   the container is deleted. **This needs OrbStack started on the laptop**
   (Notion lists local practice as optional; approving this task agrees to
   it). Nothing in production is touched, and it
   rehearses the move to the home server, which is the same steps. The R2 keys
   reach the laptop in memory only (`railway run -s backup`), as in Step 8
9. **Templates in the bucket are not backed up.** Postgres is the source of
   truth; the images are small and can be re-uploaded (the rule in CLAUDE.md).
   After a restore without the bucket, a template's row exists but its image
   is gone; the runbook says to delete such rows. Railway offers no bucket
   backups, and copying the bucket nightly would double this step for files we
   can lose
10. **Not encrypted, for now.** The dump holds emails and password hashes
    (bcrypt) and token hashes, never plain passwords or tokens. The bucket is
    private and its key is scoped to it. Encrypting with `age` would add a
    private key that, if lost, makes every backup useless; worth it on the
    home server, not now. Written in the runbook as a known gap
11. **Railway's own Backups tab** is the open question from Notion. The SDK
    has `backupSchedules` on a volume mount, so if Hobby offers it, daily
    snapshots of the volume can be declared in `.railway/railway.ts` as a
    second, faster way back (restoring a whole volume in a few clicks). The R2
    dump stays either way: a Railway snapshot lives at Railway, so an account
    problem takes it and the data together. Checked by the owner (below)
    before PR 1; added only if offered

## Files

```
ops/backup/
├── Dockerfile      # FROM postgres:18, + curl, the two scripts
├── backup.sh       # one nightly run (decision 4)
└── restore.sh      # dump file + empty Postgres → app role, restore, compare counts
docs/runbook.md     # restore, roll back, rotate a secret
.railway/railway.ts # + service "backup"
.env.example        # + the backup variables, with comments
.github/workflows/ci.yml  # the docker job also builds ops/backup
```

CI: the `docker` job builds the second image too (no new required check, so
the `main` ruleset stays as it is), and runs `shellcheck` on the two scripts.

## Owner's clicks

Before PR 1 (10 minutes, I'll walk through each screen):

1. **Railway:** open the Postgres service and say whether there is a
   **Backups** tab (decision 11). Look only; change nothing
2. **Cloudflare → R2:** create bucket `rail-canvas-backups`, location hint
   **Asia-Pacific**. Then *Settings → Object lifecycle rules*: delete objects
   after **30 days**
3. **Cloudflare → R2 → Manage API tokens:** create token "rail-canvas backup",
   permission **Object Read & Write**, **only** bucket `rail-canvas-backups`.
   Keep the page open: it shows the Access Key ID and Secret once
4. **healthchecks.io:** free account, one check "rail-canvas backup",
   schedule **cron `0 19 * * *`, UTC**, grace **1 hour**, email alert on.
   Copy its ping URL

After PR 1 merges (the `backup` service exists):

5. **Railway → `backup` → Variables:** paste the five values from steps 2–4.
   (Allowed: their values are `preserve()`, only the names are in git)

## Order of work

1. **Spike, laptop, nothing committed:** build the image, run `backup.sh`
   against a throwaway `postgres:18` in OrbStack with the real R2 keys (in
   memory) and the real ping URL. Checks: the upload signs and lands, the
   healthchecks.io check turns green, a forced failure (wrong password) turns
   it red with the error in the email. Results written into this task
2. **PR 1: the image, the scripts, the `backup` service, CI.** Local
   `railway config plan`, then the PR's plan comment: 1 service added,
   nothing changed on `api` or Postgres, nothing destroyed. **Owner reads the
   plan; merge.** Owner fills the variables (click 5)
3. **First real run:** trigger it once with `railway cron run` (the CLI's way to
   run a cron service outside its schedule), read the log, see the file in R2 and the check go green.
   Then wait for one scheduled night and check again
4. **Restore test** (decision 8) with that night's dump. Row counts must match
   `counts.txt` (any difference must be explainable by rows written in the
   seconds after the dump, i.e. `pixels.placed_at` after the dump started). Then, in the restored copy, `SET ROLE app` with
   `pixel-club` as the tenant: its canvases and templates must be visible,
   which proves the grants and RLS policies came back too
5. **PR 2: `docs/runbook.md`**, written from what steps 3–4 actually needed:
   restore (to Railway, and to a new server), roll back a deploy, rotate each
   secret (`APP_DB_PASSWORD`, the R2 key, the bucket keys, `RAILWAY_TOKEN`)
6. Notion: tick Step 9, answer the Backups question. STATUS updated

## Out of scope

Encrypting dumps (decision 10), backing up the template bucket (decision 9),
point-in-time recovery (WAL archiving: restore to any second, not just last
night; overkill here), restoring into Railway for real (the runbook describes
it; doing it would replace production), alerts other than email.

## Risks

- **`curl --aws-sigv4` may not satisfy R2** (it wants a payload hash header).
  The spike finds out; the fallback is `rclone`, one static binary added to
  the image
- **A `preserve()` variable on a new service has no value to keep.** The plan
  shows what Railway makes of it; if it refuses, the five names start as
  empty literals in the first PR and become `preserve()` in a one-line second
  PR after the owner fills them
- **A hung run blocks every later one**: Railway skips a scheduled run while
  the previous one is still going, and never stops it. So `backup.sh` runs
  under `timeout 15m` (a dump takes seconds today); a timeout counts as a
  failure, pings `/fail`, and exits
- **A secret in the logs.** `DATABASE_URL` holds the `postgres` password and
  `HC_PING_URL` is itself a secret. The scripts never use `set -x` and never
  echo a variable; errors are logged as the step that failed
- **The dump grows with the pixel history.** Today it is tiny; the size is in
  every log line, and R2's free 10 GB is far away. Step 13's retention
  cleanup keeps it bounded
