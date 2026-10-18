# Task 015 — A Telegram bot places pixels through a Railway Function, with linked accounts

**Status: approved 2026-10-18 after a sanity check (below). Live 2026-10-18 (PR #24).**

## Live check (2026-10-18, 12:11 UTC)

The owner generated the `bot` domain (`bot-production-2161.up.railway.app`) and pasted
`TELEGRAM_SECRET_TOKEN` on `bot` and in local `.env`. Fake Telegram updates sent from the laptop
to the bot's public domain; the Function called the API over the private network
(`rail-canvas.railway.internal:8080`). A link code was made as the owner on pixel-club's canvas.

| Sent | Function answered | API |
| --- | --- | --- |
| `/place 140 140 red`, wrong secret | 401 | not called |
| `/place 140 140 red`, before linking | "Link this chat first …" | `/webhooks/bot/pixels` 404 |
| `/link BZDJ-TVY7` | "Linked as Tester 1, drawing on "new canvas" …" | `/webhooks/bot/link` 200 |
| the same code again | "That code is wrong, used or expired …" | 404 |
| `/place 140 140 red` | "Placed (140, 140) red. Next in 10 s" | 201, 56 ms; cell (140, 140) went 0 to 5 |
| `/place 141 140 blue` at once | "Wait 10 s" | 429 |
| `/place 1 1 mauve` | "Unknown colour." and the help | 400 |

The bot's log shows one line per update (`refused reason=secret`, `link status=…`,
`place status=…`), no secrets and no message text. No real Telegram bot is connected yet; the
runbook says how (BotFather, `setWebhook` with the same secret).

## Local run (2026-10-18)

Postgres 18 and Redis 8.2 in OrbStack, the API from this branch on :8095 as `app`, and the
Function started **the way Railway starts it**: the `ghcr.io/railwayapp/function-bun:1.4.0` image
given the file as base64 (its `run.sh` found no packages to install and ran `bun run index.tsx`).
Fake Telegram updates sent with `curl`:

| Sent | Function answered | API |
| --- | --- | --- |
| `/place 1 1 red`, no secret header / a wrong one | 401 | not called |
| `/place 140 140 red`, before linking | "Link this chat first: /link CODE …" | 404 |
| `/link YGNG-5EQ5` (code from `POST …/bot-link-code`) | `Linked as Demo, drawing on "Demo canvas" …` | 200 |
| the same code again | "That code is wrong, used or expired …" | 404 |
| `/place 140 140 red` | "Placed (140, 140) red. Next in 10 s" | 201; board (140, 140) = 5, `pixels.source = 'bot'` |
| `/place 141 140 blue` at once | "Wait 10 s" | 429 |
| `/place 1 1 mauve` | "Unknown colour." and the colour list | 400 |

Tests: Go (with the database and Redis) all pass; the Function's 22 `bun test`s pass in the same
image. Demo database dropped, the local `app` login removed, Redis flushed.

## What changed while building

- **The Railway plan and apply workflows now also run on `functions/**`**: they ran only on
  `.railway/**`, so a change to the Function's file alone would never have deployed
- **A colour index is read before dashes are stripped**: the test caught `-1` turning into colour 1
- **The API requires a `WEBHOOK_SECRET` of 32 characters or more** (already, since Step 10): the
  local run used `openssl rand -hex 32` values, never printed


## Sanity check (2026-10-18)

- **The API's private address needs its port:** `http://rail-canvas.railway.internal:8080` (the
  api listens on 8080 on Railway, per its `listening` log line). Without the port the call goes to
  80 and fails
- **The Function runs in Railway's own image**, `ghcr.io/railwayapp/function-bun:1.4.0` (Bun
  1.4.0, what `fn()` deploys): its `run.sh` writes the file as `index.tsx` and runs
  `bun run index.tsx`. So the tests run in that image too, locally and in CI's `docker` job
  (`--entrypoint bun ... test`), not a separately installed Bun. The server starts only when the file
  is run directly (`import.meta.main`), so the tests import its handler without opening a port.
  Bun.serve listens on `PORT`, which Railway sets
- **`displayName` dropped from the link request**: nothing stores it; the reply uses the Rail
  Canvas name
- **Private chats only**, checked from `message.chat.type`; anything else gets no reply (200,
  empty)
- **Telegram resends an update it thinks failed.** Harmless without a dedupe table: a repeated
  `/place` meets the cooldown, a repeated `/link` meets a used code
- **`bot-link-code` is rate limited** per member (5 at once, then one a minute), like the other
  creating endpoints


Plan: [Rail Canvas, Step 15](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Security and tenancy" (Bot: the Function verifies the platform, the Go endpoint also requires a
shared secret, a bot user links with a one-time code first), "Data model" (`bot_links`,
`link_codes`) and the open question "Function runtime (Step 15)". Fifth stretch step.

## The goal

In Telegram, a player types `/place 10 20 red` to the Rail Canvas bot, and the pixel appears live on
the web canvas for everyone, under that player's name and with their cooldown. Before that, once,
the player links their Telegram account: the web app shows a one-time code, the player sends
`/link K7QM-2PXD` to the bot.

```
Telegram ──HTTPS──▶ Function "bot" ──private network──▶ api ──▶ Postgres, Redis, live sockets
 (/place 10 20 red)  checks it came from Telegram        checks the shared secret,
                     turns the text into a request        the link, membership, cooldown
```

## Why this exists (and whether we need it)

**Webhooks** are how other companies' systems call yours: Stripe tells a shop "payment
succeeded", GitHub tells CI "someone pushed", Telegram tells a bot "someone typed a command".
At Grab or Stockbit, payment providers, banks and partners all reach in this way. The lessons are
always the same three:

1. **Anyone on the internet can call a webhook URL.** So the first thing it does is prove the
   caller is who it says (a signature or a secret only the two sides know), before reading anything
2. **Never trust the outside user's identity as yours.** A Telegram user id is not a Rail Canvas
   account. A one-time code typed by a logged-in user is the standard way to tie the two
   ("connect your Telegram", "link your Discord")
3. **Answer fast, always.** The caller retries on errors and timeouts; a slow or failing webhook
   gets the same message again and again

A **Railway Function** is Railway's smallest service type: one TypeScript file, no repository, no
Dockerfile, run by Bun. Good for small glue code like this. Rail Canvas does not strictly need it
(the Go API could receive Telegram directly), but the plan uses it to try the service type, and it
keeps Telegram's format out of the API: the API only ever speaks its own contract.

## Words used below

- **Bot token**: the password of a Telegram bot, given by @BotFather. Not needed in this step
- **Secret token**: a random string the owner chooses and gives Telegram when registering the
  webhook. Telegram sends it back in a header (`X-Telegram-Bot-Api-Secret-Token`) with every update,
  so the Function can tell Telegram's calls from anyone else's
- **Update**: one JSON message Telegram sends to the webhook (a command typed by someone)

## Decisions for this step

### Telegram, not Discord

1. **Telegram** (agreed with the owner). Simpler for the same lessons: a bot from @BotFather in a minute, a
   secret header to check, and the reply goes back **in the webhook's own response**, so the
   Function never needs the bot token. Discord would add Ed25519 signature checking (a good lesson)
   but also an application, slash-command registration and a 3-second reply rule. If the owner
   prefers Discord, only the Function changes; the API side below stays the same

### The Function (`bot`)

2. **`functions/webhook/index.ts`**, Bun, one file, no packages (Bun has `fetch` and
   `crypto.timingSafeEqual`). Declared in `.railway/railway.ts` with the SDK's `fn()`, which embeds
   the file in the service (no repository, so no Wait for CI to drop). Singapore, one replica.
   The Notion open question answered: Functions are Bun TypeScript, at most about 98 KB of source
3. **It checks Telegram's secret header first**, in constant time, and answers 401 to anything
   else without reading the body. Body capped at 64 KB
4. **It understands three commands**, each answered with one short message:

   | Typed | Does | Answers |
   | --- | --- | --- |
   | `/link K7QM-2PXD` | redeems the code | "Linked as <display name>. Place with /place x y colour" |
   | `/place 10 20 red` | places on the linked canvas | "Placed (10, 20) red. Next in 10 s" or "Wait 7 s" |
   | `/help`, anything else | — | the three commands and the colour names |

5. **It always answers Telegram 200** (with the reply), even when the API refuses or is down
   ("Something went wrong, try again"): a non-200 makes Telegram resend the same update for hours
6. **It calls the API over Railway's private network** (`http://rail-canvas.railway.internal:8080`),
   with `X-Webhook-Secret`. Its `WEBHOOK_SECRET` is a **reference** to the api's, so nothing new to
   paste for that. The Function needs only two variables of its own: the API's address (a
   reference) and `TELEGRAM_SECRET_TOKEN` (`preserve()`, pasted by the owner)
7. **A public domain** for the Function (Telegram, or my test script, must reach it): generated once
   by the owner in the dashboard after the merge

### Linking accounts

8. **`link_codes`** (the Notion table): `code_hash` (SHA-256, primary key), `user_id`, `space_id`,
   `canvas_id`, `expires_at`, `used_at`. The code is 8 characters from an alphabet without look-alike
   letters (`K7QM-2PXD`, 40 bits), lasts **10 minutes**, single use, redeemed in one
   `UPDATE … WHERE used_at IS NULL AND expires_at > now() RETURNING` like invites
9. **The code is made from a canvas page** ("Link Telegram" button): the link remembers **which
   canvas** `/place` draws on. Linking again from another canvas moves it. Simpler than a `/use`
   command, and a player in two spaces stays unambiguous
10. **`bot_links`** (the Notion table): `platform` + `platform_user_id` (primary key), `user_id`,
    `space_id`, `canvas_id`, `linked_at`. One Telegram account → one Rail Canvas user; a new link
    replaces the old. No RLS (an auth table, like `sessions`), scoped in Go; the canvas pair has the
    composite foreign key, so a deleted canvas removes its links
11. **Brute force:** `/link` is rate limited per Telegram user (5 a minute, Redis); with 40-bit
    codes that live 10 minutes, guessing is hopeless

### The API side

12. **Two new webhook endpoints, same secret check as Step 10:**
    - `POST /webhooks/bot/link` `{platform, platformUserId, code}` → linked user's name, or 404 (bad or used code)
    - `POST /webhooks/bot/pixels` `{platform, platformUserId, x, y, color}` → placed, 429 cooldown, 404 not linked
    The API looks the Telegram user up in `bot_links` and places as that user with
    `Pixels.PlaceAs` (active user, still a member, cooldown, `source = 'bot'`, live to every copy)
13. **The Step 10 endpoint that names a user directly is removed.** It existed until linking did;
    nothing calls it. `openapi.yaml` 0.7.0
14. **Colour names live in the palette**, next to the colours (`white`, `red`, `blue`…, the 16
    r/place names), so the bot and a future palette (Step 16) agree. The bot sends the word or a
    number; the API resolves it
15. **For the web app:** `POST /spaces/{s}/canvases/{c}/bot-link-code` (member only) → the code and
    its expiry. The canvas page gets a small "Link Telegram" button that shows the code and what to
    send
16. **Cleanup** (Step 13's rule for new tables): `link_codes` expired more than a day ago

## Testing without typing in Telegram (the owner's call)

The owner will not test in the Telegram app. Nothing needs to: **Telegram's call is plain HTTPS**,
a JSON update plus the secret header, so I send exactly what Telegram would send, with `curl` or a
small script, to the live Function. Same code path, same checks. (Discord could not be faked this
way: its calls are signed with Discord's private key, which only Discord has. One more reason for
Telegram.)

- **No real bot is created in this step.** No @BotFather, no bot token. The Function never needs the
  token anyway (it replies inside its HTTP response). Making it a real bot later is three clicks and
  one command, written in the runbook
- **Owner's part, after the merge, only:** in Railway, the `bot` service → Settings → Generate
  Domain; make a secret token (`openssl rand -hex 32`), paste it as `TELEGRAM_SECRET_TOKEN` on `bot`,
  and also put it in local `.env` as `TELEGRAM_SECRET_TOKEN=…` so my test script can read it
  without printing it (like `OWNER_PASSWORD`)
- **The live test, by me:** get a link code as the owner's account (`POST …/bot-link-code`), send a
  fake update `/link CODE` to the Function, then `/place 140 140 red`, and read the reply, the
  pixel in the board, `source = 'bot'` in the API's log, and the pixel on a listening socket.
  Plus the refusals: no secret → 401; again within the cooldown → "Wait …"

## Files

```
functions/webhook/index.ts         # the Function: check, parse, call the API, reply
functions/webhook/index.test.ts    # bun test: secret check, parsing, replies (API faked)
api/db/migrations/…_create_link_codes.sql, …_create_bot_links.sql
api/db/queries/bot.sql
api/internal/service/bot.go        # MakeLinkCode, Link, PlaceFromBot
api/internal/service/palette.go    # colour names
api/internal/handler/webhooks.go   # the two bot endpoints; the old one removed
api/openapi.yaml, web/… (Link Telegram button), .railway/railway.ts (fn "bot")
.github/workflows/ci.yml           # docker job also runs bun test on the Function, in Railway's image
CLAUDE.md, docs/runbook.md (rotating the bot token and secret token), .env.example
```

## Tests

- **Function** (`bun test`, the API replaced by a fake): wrong or missing secret header → 401 and
  the API never called; `/place 10 20 red` → the right request; `/place` with bad numbers → help,
  no call; the API's 429 → "Wait 7 s"; the API down → 200 with "try again"
- **Linking:** a code works once; an expired one, a used one, a wrong one → 404; linking again moves
  the canvas; a second Telegram account can link to the same user, one Telegram account cannot point
  at two users
- **Placing:** a linked user places with `source = 'bot'` and the cooldown applies (web and bot
  share it); not linked → 404; a suspended user or one no longer a member → 404; the pixel reaches
  sockets on both API copies (the Step 14 bus)
- **Colour names:** every palette colour has a unique name; a name, a number and a wrong word
- **Cleanup:** expired `link_codes` on both sides of the line
- Every existing test still passes, minus the removed endpoint's

## Order of work

1. Build and test locally: the API in Go as usual, the Function with Bun in a container
   (`oven/bun`, OrbStack: nothing installed on the laptop). Show it: a fake Telegram update sent to
   the local Function with `curl` → a pixel on the local canvas; without the secret → 401
2. **One pull request**: code, migrations, the `bot` Function in `.railway/railway.ts`. Its plan
   shows one service added. **Owner reads the plan; merge** (away from :50–:05)
3. Owner's part: domain, secret token (in Railway and local `.env`)
4. Check live, by me with fake Telegram updates (above): link, place, refusals
5. Notion: tick Step 15. STATUS updated

## Out of scope

Discord, a real Telegram bot (no @BotFather in this step; the runbook says how), group chats (the bot answers private chats only),
choosing the canvas from Telegram, showing the board in Telegram, unlinking from the web (linking
again replaces; a "Unlink" button is a small later addition).

## Risks

- **Railway Functions are new** (an `fn()` building block in the SDK). If one cannot be declared or
  reached as expected, the fallback from Notion applies: Telegram calls a Go endpoint on the API
  directly, with the same checks
- **The secret token is the Function's only secret.** In Railway and the owner's `.env`, never in
  git or the chat. Anyone holding it can send fake updates, but they still need a link code from a
  logged-in user to act as anyone
- **Cost:** a Function idles like a small service, about $0.20–0.30 a month
