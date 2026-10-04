# Task 005 — Place a pixel and see it live

**Status: built 2026-10-04 and pushed. 15 database tests and 4 hub tests green (simultaneous clicks 5 of 5 runs; hub tests 3 of 3 under -race). Node end-to-end check passed locally; the owner tried two windows side by side.**

## What changed from the plan

- **Default cooldown 10 seconds** everywhere (owner's call), not 30.
- **Optimistic placing** moved into scope after the owner noticed a half-second delay: draw on click, undo if refused. The delay was mostly the local API reaching Singapore over the public proxy (about 10 database round trips per placement).
- **The canvas list shows each canvas's cooldown**: an old test canvas kept its 5-minute value and surprised the owner.
- **Hub fixes found while building:** a slow client was counted as dropped on every message (now once), and shutdown closed sockets one by one (now in parallel). Close sends the frame before cancelling, or the library drops the socket without it.

Plan: [Rail Canvas, Step 6](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Realtime" and "Data model" sections.

## The goal

Click the board and the pixel is saved; every open tab on that canvas sees it
appear within a moment. Two browsers side by side, painting together. This is
the first long-lived connection in the app, so it also brings the robustness
rules: tickets, reconnect, slow clients, pings, clean shutdown, and the stats
line every later step reads.

## Decisions for this step

1. **Placing goes through REST, never the socket.**
   `POST /spaces/{spaceId}/canvases/{canvasId}/pixels`. The row is the truth and
   the WebSocket message is a nudge: a missed message costs nothing a board
   reload does not fix.
2. **One transaction per placement, in this order:**
   1. claim the cooldown: `UPDATE space_members SET last_placed_at = now()
      WHERE … AND (last_placed_at IS NULL OR last_placed_at <= now() - cooldown)
      RETURNING …`. No row back means "too soon" → 429 with the time of the next
      allowed placement. Atomic, so two fast clicks cannot both pass (tested)
   2. insert into `pixels` (history), returning its id
   3. upsert `canvas_pixels` with the color and that id
   4. commit, **then** publish `pixel:placed` — never announce a pixel that
      might roll back
3. **The cooldown is per member per space**, using the canvas's
   `cooldown_seconds`. One clock for the whole space, like r/place.
4. **`cooldownSeconds` becomes optional on canvas creation** (1 to 3600). The
   default drops from 300 to **10 seconds** everywhere, staging included (the
   owner's call: a personal project, a small group). A migration changes the
   column default.
5. **`canvas_pixels.pixel_id` gets no foreign key to `pixels`.** The plan said it
   would, but the nightly cleanup (Step 13) deletes old history, and a foreign
   key would block deleting any pixel still showing on the board. `pixel_id` is a
   version number for ordering, not a reference. Notion gets corrected.
6. **WebSocket login by ticket.** Browsers cannot send `Authorization` on a
   WebSocket. `POST /ws/ticket` (logged in) returns a random ticket, valid 30
   seconds, usable once, stored as SHA-256 in `ws_tickets`. The page opens
   `/ws?ticket=…`. Redeeming is one atomic `UPDATE … RETURNING`, like invites.
   Single use makes it harmless if the URL lands in a log. Our request log
   records the path only, not the query.
7. **Library: `github.com/coder/websocket`**, as the RFC chose. Its `Accept`
   checks `Origin` against the same allowed origins as CORS.
8. **Messages** (JSON text frames, 1 KB max from the client):
   - client → server: `subscribe` / `unsubscribe` with `spaceId` and `canvasId`.
     Subscribing checks membership and that the canvas is in that space
   - server → client: `subscribed`, `pixel:placed` (`canvasId`, `pixelId`, `x`,
     `y`, `color`), `error`
9. **The hub** (`internal/realtime`) maps topic `canvas:<id>` to connected
   clients, behind a `Broadcaster` interface so Step 14 can swap in Redis
   pub/sub without touching services.
10. **A slow client never stalls the others.** Each client has a send buffer of
    64 messages; when it is full the client is disconnected (it reconnects and
    reloads). The hub never waits on one socket.
11. **Pings every 30 seconds**, closing after 10 seconds without an answer. Finds
    dead connections, and keeps proxies from dropping idle ones.
12. **Clean shutdown.** `http.Server.Shutdown` does not close WebSockets (they
    leave the server's hands on upgrade), so on SIGTERM the hub closes every
    socket with "going away" and clients reconnect at once to the new deploy.
13. **The stats line**, every 30 seconds in the log: WebSocket clients,
    subscriptions, slow clients dropped, pool connections in use / idle / max,
    and how often a request had to wait for a connection. Steps 11 to 17 are
    measured against it.

## Frontend

- **Zoom and pan, so a pixel is big enough to aim at.** At 256×256 a cell is
  about 3 screen pixels when the board fits the window.
  - zoom with the mouse wheel, a trackpad pinch, or +/− buttons, from
    fit-to-screen up to about 40× (a cell becomes 40 screen pixels)
  - drag to pan when zoomed in; a click places, a drag pans, never both
  - grid lines once a cell is at least 8 screen pixels wide
  - the hovered cell is outlined in the selected color, with its x and y shown
  - still one `<canvas>`: zoom and pan are a CSS transform, the board itself
    is never redrawn for them
- **Click to paint.** The click sends the placement; on success the cell is
  drawn and a cooldown countdown starts. Too soon (429) shows the countdown too.
- **Live updates.** `useLiveCanvas` opens the socket and draws incoming pixels
  one cell at a time (`fillRect`), not by redrawing the whole board.
- **No pixel lost on connect.** Order: get a ticket, connect, subscribe, then
  load the board, holding any pixels that arrive meanwhile and replaying them
  after. Each cell remembers the newest `pixelId` it has drawn and ignores
  older ones.
- **Reconnect** with growing waits (1, 2, 4 … up to 30 seconds, with jitter),
  a fresh ticket each time, and a board reload.
- **A small status dot:** live / reconnecting.

## Migrations

| # | Contents | RLS |
| --- | --- | --- |
| 1 | `pixels (id bigint identity, space_id, canvas_id, x, y, color, user_id, source web/bot, placed_at)`, foreign key `(canvas_id, space_id)`, index `(canvas_id, placed_at)` | Yes, forced |
| 2 | `ws_tickets (ticket_hash, user_id, expires_at, used_at)` | No |
| 3 | `canvases.cooldown_seconds` default 300 → 10 (existing canvases keep theirs) | — |

## Endpoints

| Method and path | Who | What |
| --- | --- | --- |
| `POST /spaces/{spaceId}/canvases/{canvasId}/pixels` | member | `{x, y, color}` → the placed pixel and `nextPlaceAt`; 429 with `nextPlaceAt` when too soon |
| `POST /spaces/{spaceId}/canvases` | member | gains optional `cooldownSeconds` |
| `POST /ws/ticket` | logged in | `{ticket, expiresAt}` |
| `GET /ws?ticket=…` | ticket holder | WebSocket. Mounted directly on the router, outside the generated interface (an upgrade hands the connection over, which the strict server cannot express) |

## Tests

Database (as `app`):

- a placement shows on the board and in the history
- a second placement inside the cooldown is refused with the right `nextPlaceAt`
- **two placements fired at once by one member: exactly one succeeds**
- out-of-range `x`, `y` or color is refused; another space's canvas is not found
- RLS on `pixels`: no tenant → no rows
- a ticket works once; a used, expired or made-up ticket does not

Realtime:

- two WebSocket clients subscribed to a canvas both receive a placement;
  a client on another canvas does not
- a non-member's subscribe is refused
- a client that stops reading is dropped once its buffer fills, and the others
  keep receiving
- shutdown closes sockets with "going away"

## Order of work

1. Migrations, services, hub, handlers, tests — **no database touched**
2. **Ask the owner**, then: migrations on Railway and the test database, `make test-db`
3. Frontend; run locally (two browser windows) and show the owner
4. Push; check live with two browsers; read the stats line in Railway's logs;
   redeploy once while connected and watch both tabs reconnect on their own

## Out of scope

Redis cache and cooldown (Step 11), more than one API replica (Step 14), the bot
(Step 15), an optimistic draw before the server answers, touch gestures beyond
pinch-zoom.
