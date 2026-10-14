# Task 008 — Templates: upload a reference image to a bucket, overlay it on the board

**Status: approved 2026-10-14. Pull request 1 (the bucket) open.**

Plan: [Rail Canvas, Step 8](https://app.notion.com/p/3ee95a71540581c8af9ac997375758e9) and its
"Data model", "Security and tenancy" (Uploads) and "Open questions" (Bucket CORS) sections.
Docs: [Railway Storage Buckets](https://docs.railway.com/storage-buckets),
[IaC reference](https://docs.railway.com/infrastructure-as-code/reference) (`bucket()`).

## The goal

A member uploads a small PNG (the picture the group wants to paint) to a
canvas. Everyone on that canvas can show it as a see-through overlay on the
board, at the position it was placed, and paint over it cell by cell, the way
r/place communities coordinate. The image lives in a private Railway bucket; the
browser uploads and downloads it directly with short-lived signed URLs, so the
image bytes never pass through the API.

## What a bucket is, in this project

Railway's Storage Bucket is private, S3-compatible file storage (run on Tigris).
Nothing in it is public. To let a browser read or write one file, the API
**signs a URL**: a link that works for one file, one action (upload or download)
and a few minutes, then expires. The API holds the bucket's keys; the browser
only ever sees signed URLs.

Facts from Railway's docs (2026-10-14) that shape the plan:

- private only, no public bucket; presigned URLs are the supported way in
- reached over the public internet only (no private network), at
  `https://t3.storageapi.dev`
- the service gets `BUCKET`, `ACCESS_KEY_ID`, `SECRET_ACCESS_KEY`, `REGION`,
  `ENDPOINT` as variable references
- the region is chosen at creation and can never change
- no backups, no versioning; a deleted bucket can be restored for 52 hours
- storage $0.015 per GB-month, bucket egress free; uploads that go through a
  service are billed as service egress, another reason to upload directly
- **CORS is not documented.** A browser uploading straight to the bucket needs
  the bucket to allow our site's origin. Unknown until tried (decision 3)

## Decisions for this step

1. **The bucket is created through `.railway/railway.ts`**, not the dashboard:
   `bucket("templates", { region })`, and the `api` service gets the five
   variables as references to it, prefixed so they read clearly in our config
   (and so the AWS SDK does not pick them up by itself): `S3_BUCKET`,
   `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_REGION`, `S3_ENDPOINT`. Its own pull request, so Railway's plan shows
   "1 to add" (plus the variables) and nothing else. The region: Singapore if
   the plan accepts it, since the API runs there; if not, the nearest offered.
   It cannot be moved later, so this is checked before merging.
2. **Direct upload with a signed URL, in three calls:**
   1. `POST …/templates` with the file's size, type and the position on the
      board → the API checks the limits, writes a `templates` row with status
      `pending`, and returns a signed upload URL valid **5 minutes**
   2. the browser `PUT`s the file straight to the bucket
   3. `POST …/templates/{id}/complete` → the API checks the stored file (below)
      and marks it `ready`, or deletes it and marks it `rejected` with a reason
3. **CORS is the first thing tested, before any app code.** If the bucket
   accepts a CORS rule for our origins (Pages, its previews, localhost), plan
   as above. If it does not, the fallback from Notion: the browser sends the
   file to the API, which checks it and puts it in the bucket. Same endpoints
   from the browser's point of view except call 2; the API pays the egress
   (tiny at 1 MB).
4. **An upload is untrusted. Checks, in order:**
   - in `POST …/templates`: type must be `image/png`, size at most **1 MB**
     (the API refuses before signing anything)
   - the signed URL fixes the size and type, so the bucket rejects a different
     file. **Whether Tigris enforces the signed size is tested in the spike**;
     if it does not, decision 5 covers it
   - in `complete`: `HeadObject` confirms the stored size is at most 1 MB, then
     only the first bytes are read and `image.DecodeConfig` checks it is a PNG
     of at most **256 × 256** (the canvas size), without decoding the pixels. A
     decompression bomb is a small file that claims huge dimensions; it is
     refused here, before anything decodes it. Step 8 never decodes the full
     image on the server (thumbnails are the worker's job in Step 12)
5. **Abandoned or oversized uploads cannot pile up.** A `pending` row older than
   an hour is cleaned up by Step 13's nightly cleanup; until then, at most
   **10 template uploads per member per hour** (in-memory limiter, like login),
   and a canvas holds at most **20 ready templates**.
6. **Downloads are signed too.** `GET …/templates` lists the canvas's ready
   templates, each with a signed download URL valid **1 hour**. The overlay is
   a plain `<img>`, so downloading needs no CORS: a cross-origin image can be
   shown, just not read back by script, and nothing here reads it.
7. **Tenancy.** `templates` has `space_id`, RLS (`ENABLE` + `FORCE`, one
   policy), and the composite foreign key `(canvas_id, space_id)`. The object
   key is `spaces/{spaceId}/templates/{templateId}/original`, built by the API
   from the URL's space and a new id, never from anything the client sends. A
   signed URL names exactly one key, so a member can never touch another
   space's files.
8. **Who can delete:** the uploader or the space's owner. Deleting removes the
   object first, then the row.
9. **The template has a position** (`x`, `y`, the top-left cell where it sits),
   chosen at upload (0 to 255 each), so it can sit anywhere on the board. A
   template that runs past the board's edge is clipped by the overlay, not
   refused. Not in Notion's data model; added here, Notion gets updated.
10. **The bucket client sits behind an interface** (`service.ObjectStore`:
    sign upload, sign download, head, read first bytes, delete), so service
    tests run with a fake and need no bucket. The real one is in
    `platform/bucket.go` using `aws-sdk-go-v2` (the standard S3 client).
11. **The API still starts without a bucket** (locally, and in CI): the bucket
    variables are optional, and the template endpoints answer 503 "templates
    are not configured" when they are missing. Production must have them; the
    boot log says which mode it is in.

## Data model

| Table | Columns | RLS |
| --- | --- | --- |
| `templates` | `id` uuid, `space_id`, `canvas_id`, `uploaded_by`, `object_key`, `thumb_key` (null until Step 12), `x`, `y`, `width`, `height` (set by `complete`), `bytes`, `status` (`pending`, `ready`, `rejected`), `reject_reason`, `created_at` | Yes, forced |

Index on `(canvas_id, status)`. One migration.

## Endpoints

| Method and path | Who | What |
| --- | --- | --- |
| `POST /spaces/{spaceId}/canvases/{canvasId}/templates` | member | `{bytes, contentType, x, y}` → `{templateId, uploadUrl, uploadHeaders, expiresAt}`; 400 over the limits, 429 over the rate, 409 canvas full |
| `POST …/templates/{templateId}/complete` | the uploader | → the template (`ready`), or 422 with the reason (`rejected`) |
| `GET /spaces/{spaceId}/canvases/{canvasId}/templates` | member | ready templates, newest first, each with `url` and `urlExpiresAt` |
| `DELETE …/templates/{templateId}` | uploader or owner | 204 |

## Frontend

- A **Templates** panel beside the palette: an upload button (PNG only), a
  position (`x`, `y`, defaulting to the top-left of the current view), and the
  list of the canvas's templates
- The browser checks size and dimensions before uploading, for a quick message;
  the server's checks are the real ones
- Each template can be shown or hidden, with an opacity slider. Shown templates
  are drawn as an `<img>` inside the board's zoom/pan layer, with
  `image-rendering: pixelated`, so one image pixel lines up with one cell
- Layers as usual: `client.ts` (including the direct `PUT` to the signed URL,
  the one call that is not to our API) → `useTemplates` / `useUploadTemplate`
  hooks → components
- Freshness: the list is fetched when the canvas opens and after an upload or
  delete; the signed URLs last an hour, and the list is refetched before then

## Owner's clicks

None in the dashboard: the bucket, its region and the variables come from
`.railway/railway.ts` and go through a plan comment. The owner reads that plan
before it is merged, because the region cannot be changed afterwards.

## Order of work

1. **Pull request 1, the bucket only.** `bucket("templates", …)` and the five
   variables on `api` in `.railway/railway.ts`. Local plan, then the PR's plan
   comment: 1 bucket added, `api` variables added, nothing destroyed. **Owner
   reads the plan; merge.** The API ignores the variables until step 3
2. **Spike, from the laptop, nothing committed:** with the bucket's
   credentials (read through the Railway CLI, never written to a file), set a
   CORS rule for our origins; sign an upload URL and `PUT` to it from a page on
   `localhost:5173`; try a file larger than the signed size; sign and open a
   download URL. Write the results into this task, and pick decision 3's path
3. **Pull request 2, the feature:** migration, sqlc queries, `openapi.yaml`,
   `platform/bucket.go`, service, handlers, frontend, tests. **No database
   touched by hand:** the migration runs in the pre-deploy step
4. Show the owner locally: `railway run` hands the bucket's variables to the
   API on the laptop in memory (never in `.env`), against a throwaway local
   Postgres; uploads land under that local space's own path in the bucket and
   are deleted afterwards. Then merge
5. Check live: upload a template on the site, see it in a second browser, hide
   and show it, delete it; look at the bucket's Files tab: one object at the
   expected path, gone after the delete
6. Notion: tick Step 8, answer the Bucket CORS question, add `x`, `y` to the
   `templates` row. STATUS updated

## Tests

Database (as `app`), with the fake store:

- create: limits refused before signing (type, size); the 10-per-hour limit;
  the 20-per-canvas limit; another space's canvas is not found
- complete: a valid PNG becomes `ready` with its width and height; too big,
  not a PNG, or bigger than 256 × 256 becomes `rejected` and the object is
  deleted; only the uploader may complete; completing twice is refused
- a PNG header claiming 50 000 × 50 000 is refused without decoding (the bomb
  case)
- list: only `ready` templates of this canvas, with signed URLs
- delete: the uploader and the owner may; another member may not
- RLS on `templates`: no tenant, no rows
- the object key always starts with `spaces/{spaceId}/templates/`

## Out of scope

Thumbnails (Step 12's worker), cleaning up abandoned uploads (Step 13's nightly
cleanup), formats other than PNG, templates larger than the canvas, editing a
template's position after upload, announcing new templates over the WebSocket
(the list refetches instead), the canvas snapshots that will share this bucket
(Step 10).

## Risks

- **Singapore may not be offered for buckets.** Then the nearest region; the
  API reaches the bucket over the public internet either way, so the cost is
  latency on `complete` only.
- **CORS may not be settable** on Railway's buckets: the upload-through-the-API
  fallback is planned (decision 3).
- **The signed size may not be enforced** by Tigris: `complete` still checks
  the stored size and deletes an oversized file, and the rate limit caps how
  many a member can try.
