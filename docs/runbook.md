# Runbook

What to do when something breaks. Written 2026-10-15, after the first real backup and restore.
Keep each section short enough to follow at 2 a.m.; fix it here when reality differs.

- [Backups: where they are, how to check them](#backups)
- [Reach the database from the laptop (temporarily)](#reach-the-database-from-the-laptop-temporarily)
- [Restore production from a backup](#restore-production-from-a-backup)
- [Restore onto a new server](#restore-onto-a-new-server)
- [Roll back a deploy](#roll-back-a-deploy)
- [Rotate a secret](#rotate-a-secret)

---

## Backups

- **What:** every night at 19:00 UTC (03:00 Singapore), Railway's cron service `backup` dumps
  Postgres with `pg_dump` and uploads it to Cloudflare R2, bucket `rail-canvas-backups`, as
  `postgres/YYYY/MM/DD/HHMMSS-railway.dump` plus `….counts.txt` (rows per table at backup time).
  Code: `ops/backup/`. R2 deletes files after 30 days.
- **Is it working?** healthchecks.io, check `rail-canvas backup`: green, last ping this morning.
  It emails when a night fails (the email says which step) or never reports.
- **Run one now:** `railway cron run --service backup --yes`, then `railway cron list --service backup`
  and the logs it points to. A good run ends with `backup done`.
- **Not backed up:** the template images in Railway's `templates` bucket (they can be re-uploaded;
  after a restore without them, delete the template rows whose image is gone). Railway's own
  volume backups need the Pro plan.
- **Not encrypted:** the dumps hold emails and bcrypt / SHA-256 hashes, never plain passwords or
  tokens. The bucket is private and its key reaches only that bucket.

## Reach the database from the laptop (temporarily)

The database has no public address on purpose. A restore or a role change from the laptop
needs one for a few minutes:

1. Railway → `Postgres` → Settings → Networking → **TCP Proxy** → add (port 5432)
2. Railway → `Postgres` → Variables → `DATABASE_PUBLIC_URL` is the address (user `postgres`).
   Use it in your own terminal only; never paste it into a file or chat
3. **Afterwards, delete the TCP Proxy.** It is not in `.railway/railway.ts`, so while it exists
   the next `.railway/` pull request would plan to remove it: delete it first

## Restore production from a backup

When data is lost or damaged (a bad migration, a bad delete). Takes about 15 minutes.
The idea: restore into a **new database beside the old one**, check it, then swap names, so
the damaged copy is kept until you are sure.

1. **Pick the backup.** R2 → `rail-canvas-backups` → `postgres/…`. The newest before the damage.
   Its key is the path without `.dump`, e.g. `postgres/2026/10/15/160811-railway`
2. **Stop the API, so nothing writes meanwhile.** Railway → `api` → the active deployment →
   ⋮ → **Remove**. The site shows errors until step 7
3. **Open the database to the laptop** (section above). Start OrbStack
4. **Make an empty database** (in your terminal; `$URL` is `DATABASE_PUBLIC_URL`):
   ```
   psql "$URL" -c 'CREATE DATABASE railway_restored'
   ```
5. **Restore and check** (from the repo; the R2 keys come from the `backup` service, in memory):
   ```
   docker build -t rail-canvas-backup ops/backup
   TARGET_URL="${URL%/*}/railway_restored" railway run -s backup -- \
     docker run --rm -e TARGET_URL -e R2_ACCOUNT_ID -e R2_BUCKET -e R2_ACCESS_KEY_ID -e R2_SECRET_ACCESS_KEY \
     rail-canvas-backup restore.sh postgres/2026/10/15/160811-railway
   ```
   It must end with `row counts match`. (`latest` instead of a key takes the newest)
6. **Swap the names**, keeping the damaged one:
   ```
   psql "${URL%/*}/postgres" -c 'ALTER DATABASE railway RENAME TO railway_damaged' \
                             -c 'ALTER DATABASE railway_restored RENAME TO railway'
   ```
   If it says the database is in use: the API is still running (step 2), or the backup is
   running (it takes seconds; try again)
7. **Start the API:** Railway → `api` → Deployments → the newest → ⋮ → **Redeploy**. The
   pre-deploy step migrates the restored copy if it is behind. Check the site
8. **Close the database again** (delete the TCP Proxy). Drop `railway_damaged` after a week:
   `DROP DATABASE railway_damaged`

The `app` role and its password live on the server, not in the database, so they survive a
restore on the same server.

## Restore onto a new server

The home server, or a new Railway project. Same tools: Postgres **18 or newer**, and the
`ops/backup` image.

1. Create an empty database (`createdb railway`)
2. `restore.sh` as in step 5 above, with `TARGET_URL` pointing at it as a superuser. It creates
   the `app` role first, because `pg_dump` leaves roles out
3. Let `app` log in: `ALTER ROLE app WITH LOGIN PASSWORD '…'` with a new password, and give
   the API `DATABASE_URL=postgres://app:…@host:5432/railway`. The `postgres` user's URL goes to
   the pre-deploy step (`MIGRATE_DATABASE_URL`) and the backup (`DATABASE_URL`)
4. Re-upload template images, or delete their rows

## Roll back a deploy

When a deploy broke the site.

- **Preferred: revert the pull request.** GitHub → the merged PR → **Revert** → merge the
  revert PR. CI runs, Railway deploys the old code the normal way
- **Fast: Railway rollback.** Railway → `api` → Deployments → the last good one → ⋮ →
  **Rollback**. Live in about a minute. It also restores *that* deployment's settings, and an
  older binary applies no migrations; that is safe because every migration is
  backward-compatible. Then fix forward with a PR; the next merge deploys over the rollback
- A rollback never touches the database. If data is damaged, see Restore

## Rotate a secret

When a secret leaked (pasted into a chat, a screenshot, a public file). Always: make the new
one, switch to it, check, **then** delete the old one.

| Secret | Where it lives | How to replace it |
| --- | --- | --- |
| R2 key | `backup` variables `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | Cloudflare → R2 → Manage API tokens → new Account token, Object Read & Write, only `rail-canvas-backups`. Paste both into `backup`'s variables, run a backup now, then delete the old token |
| healthchecks ping URL | `backup` variable `HC_PING_URL` | healthchecks.io → make a new check with the same schedule (cron `0 19 * * *`, UTC, grace 1 hour), paste its URL, run a backup now (the new check turns green), delete the old check |
| `RAILWAY_TOKEN` | GitHub secret, used by the Railway plan and apply workflows | Railway → project Settings → Tokens → new token for `production`. GitHub → repo Settings → Secrets → Actions → update `RAILWAY_TOKEN`. Delete the old token in Railway |
| `app` password | `api` variable `APP_DB_PASSWORD`, inside `DATABASE_URL` | Open the database (section above). `ALTER ROLE app WITH PASSWORD '…'` as `postgres`, then the same value in `APP_DB_PASSWORD`; the API redeploys with it. A few seconds of errors in between |
| `postgres` password | the `Postgres` service; referenced by `api` (`MIGRATE_DATABASE_URL`) and `backup` | Railway → `Postgres` → its credentials / Variables (Railway updates the user and the references together where it offers that). Then redeploy `api` and run a backup to prove both still connect |
| Template bucket keys | the `templates` bucket, referenced by `api` (`S3_*`) | Railway → `templates` → Credentials → reset, if offered. The references update; redeploy `api` and upload a template to check |
| A user's password | `users.password_hash` | No endpoint yet. Sessions can be revoked in the database (`UPDATE sessions SET revoked_at = now() WHERE user_id = …`) |

Never put a secret in git, in `.env.example`, in a chat or in a screenshot.
