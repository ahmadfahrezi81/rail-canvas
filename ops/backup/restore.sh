#!/bin/bash
# Restore a backup from R2 into an empty database, then compare row counts with the
# ones taken at backup time.
#   restore.sh latest | restore.sh postgres/2026/10/15/190000-railway
# Needs R2_* and TARGET_URL: an empty database, connected as a superuser.
set -Eeuo pipefail
source /usr/local/bin/lib.sh

require R2_ACCOUNT_ID R2_BUCKET R2_ACCESS_KEY_ID R2_SECRET_ACCESS_KEY TARGET_URL
key=${1:?usage: restore.sh latest | restore.sh <key without .dump>}

if [[ "$key" == latest ]]; then
  # Keys sort by date (postgres/YYYY/MM/DD/HHMMSS-db), so the last one is the newest.
  key=$(r2_curl "$(r2_url)?list-type=2&prefix=postgres/" |
    grep -o '<Key>[^<]*\.dump</Key>' | sed -e 's/<Key>//' -e 's/\.dump<\/Key>//' | sort | tail -1)
  [[ -n "$key" ]] || { log error "no backups in the bucket"; exit 1; }
fi
log info "restoring" ",\"key\":\"$key.dump\""

r2_curl --output /tmp/restore.dump "$(r2_url "$key.dump")"
r2_curl --output /tmp/restore.counts.txt "$(r2_url "$key.counts.txt")"

# pg_dump leaves roles out; the dump's GRANTs to app fail without it.
psql "$TARGET_URL" -X -q -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app') THEN
        CREATE ROLE app NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
    END IF;
END
$$;
SQL

pg_restore --exit-on-error --single-transaction --dbname="$TARGET_URL" /tmp/restore.dump
log info "restored"

psql "$TARGET_URL" -X -A -t -v ON_ERROR_STOP=1 -c "$COUNTS_SQL" >/tmp/restored.counts.txt
if diff <(sort /tmp/restore.counts.txt) <(sort /tmp/restored.counts.txt) >/tmp/counts.diff; then
  log info "row counts match" ",\"tables\":$(wc -l </tmp/restored.counts.txt)"
else
  log error "row counts differ; lines with < are the backup's, > the restored copy's"
  cat /tmp/counts.diff
  exit 1
fi
