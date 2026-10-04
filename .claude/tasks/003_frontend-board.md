# Task 003 — The board in a browser

**Status: live 2026-10-04 at https://rail-canvas.pages.dev. CORS checked on the live API: the Pages URL and a preview subdomain are allowed, a foreign origin is not.**

Found while building: TypeScript 7 dropped the JavaScript API `openapi-typescript` uses, so the frontend pins TypeScript 5.x.

Plan: [Rail Canvas, Step 4](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9).

## The goal

Open a URL, see the canvas. A Vite + React app on Cloudflare Pages that lists
the dev space's canvases, draws the selected board from
`GET /canvases/{id}/board`, and shows the palette as a color picker. Nothing is
placed yet (Step 6) — the win is the full path browser → Pages → API →
Postgres, with types generated from `openapi.yaml`.

Light on purpose: this is not a frontend project. No UI library, no router,
plain CSS.

## Decisions for this step

1. **Stack.** Vite 8, React 19, TypeScript, pnpm (Node 22 and pnpm 10 are
   already installed). One page, no router.
2. **Types from the contract.** `openapi-typescript` generates
   `web/src/lib/api/schema.d.ts` from `api/openapi.yaml`, and `openapi-fetch`
   (about 6 KB) gives a typed client over it. `make generate` now regenerates
   both sides, so a spec change that breaks the frontend fails `pnpm build`.
3. **Same layers as tea-pos, thinner.**
   - `src/lib/api/` — the api client: URL, method, typed result. Nothing else
     calls `fetch`.
   - `src/lib/hooks/` — `useCanvases`, `useBoard`, built on SWR. Components
     call hooks, never the client.
   - `src/components/` — `CanvasList`, `Board`, `Palette`.
4. **The board is drawn, not rendered as 65,536 elements.** The bytes go into
   an `ImageData` on one 256×256 `<canvas>`, scaled up with CSS
   `image-rendering: pixelated`. One draw per board load.
5. **The picker is real, placing is not.** Clicking a swatch selects a color,
   clicking the board shows the cell's x and y. `POST /pixels` arrives in Step 6.
6. **API address at build time.** `VITE_API_URL`, set in Cloudflare Pages and
   in `web/.env.local` for local runs. It is public (it ships in the
   JavaScript), so it is a setting, not a secret.
7. **CORS on the API.** Browsers block a page on `*.pages.dev` from reading
   `*.railway.app` unless the API allows it. A `cors` middleware
   (`github.com/go-chi/cors`) allows only the origins in `CORS_ORIGINS`: the
   Pages URL, its preview subdomains (`https://*.rail-canvas.pages.dev`) and
   `http://localhost:5173`. The `Authorization` header is allowed now, for
   Step 5's bearer token.
8. **Pages only rebuilds for frontend changes.** Build watch paths `web/**`
   and `api/openapi.yaml`, so an API-only push does not rebuild the site.

## Files

```
web/
├── package.json, pnpm-lock.yaml, tsconfig.json, vite.config.ts, index.html
├── .env.example                  # VITE_API_URL
└── src/
    ├── main.tsx, App.tsx, app.css
    ├── lib/api/
    │   ├── schema.d.ts           # generated, never edited
    │   └── client.ts             # openapi-fetch client + canvasesApi
    ├── lib/hooks/
    │   ├── useCanvases.ts
    │   └── useBoard.ts
    └── components/
        ├── CanvasList.tsx
        ├── Board.tsx
        └── Palette.tsx
api/
└── internal/middleware/cors.go   # CORS_ORIGINS
```

## Owner steps (Cloudflare and Railway)

Given in full when the code is ready:

1. Railway `api` → Variables: `CORS_ORIGINS` (exact value given then)
2. Cloudflare → Workers & Pages → Create → Pages → Connect to Git →
   `rail-canvas`. Root directory `web`, build command `pnpm build`, output
   `dist`, variable `VITE_API_URL=https://rail-canvas-production.up.railway.app`
3. Build watch paths: `web/**`, `api/openapi.yaml`

**Check while there:** Cloudflare has been steering new projects from Pages to
Workers with static assets. If Pages is not offered for a new project, the same
build settings work as a Worker with static assets, and only the URL changes.

## Known gap until Step 5

The API has no login yet, so anyone who finds its URL can list and create
canvases in the dev space. Low stakes (a name, nothing else), and Step 5
closes it. If it gets abused before then, unset `DEV_SPACE_ID` and every
canvas route returns 401.

## Order of work

1. Scaffold `web/`, generate types, client, hooks, components — **no deploy**
2. CORS middleware with a test, `make generate` and `make check` cover `web/`
3. Run locally against the live API (needs `CORS_ORIGINS` to include
   `localhost:5173`) and show the owner
4. Owner: Railway variable, Cloudflare Pages project
5. Push, check the Pages URL draws "First canvas"

## How we know it works

- `pnpm build` passes with strict TypeScript; `make check` green
- CORS test: an allowed origin gets `Access-Control-Allow-Origin`, any other
  origin does not
- Locally and on Pages: the canvas list shows "First canvas", the board draws
  (all white, since nothing is painted), clicking shows coordinates, the
  palette shows 16 colors
- Browser devtools: one request for the list, one for the board, no CORS errors
