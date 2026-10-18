#!/usr/bin/env bash
# Logs the seeded players in once and keeps their session tokens (30 days) in .tokens.json, so
# every run reuses them. Paced under the API's per-IP login limit (20 at once, then one per 3 s).
# Usage: STAGING_URL=… SEED_OWNER_PASSWORD=… PLAYERS=100 ./login.sh. Prints no password or token.
set -euo pipefail
cd "$(dirname "$0")"

: "${STAGING_URL:?}" "${SEED_OWNER_PASSWORD:?}"
players=${PLAYERS:-100}
case "$STAGING_URL" in
  *staging*|http://localhost*|http://host.docker.internal*) ;;
  *) echo "refusing $STAGING_URL: staging or a local API only" >&2; exit 1 ;;
esac

umask 077
out=$(mktemp)
echo "[" >"$out"
for i in $(seq 1 "$players"); do
  email=$(printf 'player-%03d@example.com' "$i")
  body=$(jq -nc --arg e "$email" --arg p "$SEED_OWNER_PASSWORD" '{email: $e, password: $p}')
  for _ in 1 2 3 4 5; do
    code=$(curl -s -o "$out.res" -w '%{http_code}' -H 'content-type: application/json' -d "$body" "$STAGING_URL/auth/login")
    [ "$code" = 200 ] && break
    [ "$code" = 429 ] || { echo "$email: login answered $code" >&2; rm -f "$out" "$out.res"; exit 1; }
    sleep 3
  done
  [ "$code" = 200 ] || { echo "$email: still rate limited" >&2; rm -f "$out" "$out.res"; exit 1; }
  [ "$i" -gt 1 ] && echo "," >>"$out"
  jq -c .token "$out.res" >>"$out"
  [ "$i" -ge 15 ] && sleep 3.1
  printf '\rlogged in %d/%d' "$i" "$players"
done
echo "]" >>"$out"
mv "$out" .tokens.json
rm -f "$out.res"
echo; echo "wrote .tokens.json ($players tokens)"
