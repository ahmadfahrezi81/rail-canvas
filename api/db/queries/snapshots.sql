-- name: ListSpaceIDs :many
-- Across tenants: spaces has no RLS. Background jobs only.
SELECT id FROM spaces ORDER BY id;

-- name: ListCanvasIDs :many
SELECT id FROM canvases WHERE space_id = sqlc.arg(space_id) ORDER BY id;

-- name: ListCanvasCells :many
-- With pixel_id, so a snapshot and its newest pixel come from one read.
SELECT x, y, color, pixel_id FROM canvas_pixels
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id);

-- name: LatestSnapshotPixelID :one
SELECT last_pixel_id FROM snapshots
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
ORDER BY taken_at DESC
LIMIT 1;

-- name: ClaimSnapshot :execrows
-- 0 rows: this hour's snapshot exists already (another API version took it).
INSERT INTO snapshots (space_id, canvas_id, object_key, last_pixel_id, taken_at)
VALUES (sqlc.arg(space_id), sqlc.arg(canvas_id), sqlc.arg(object_key), sqlc.arg(last_pixel_id), sqlc.arg(taken_at))
ON CONFLICT DO NOTHING;

-- name: ListSnapshots :many
SELECT id, object_key, taken_at FROM snapshots
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
ORDER BY taken_at DESC
LIMIT sqlc.arg(max_rows);
