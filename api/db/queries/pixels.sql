-- name: ClaimCooldown :one
-- Atomic: of two concurrent claims, the second sees the first's timestamp and matches nothing.
UPDATE space_members SET last_placed_at = now()
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id)
  AND (last_placed_at IS NULL OR last_placed_at <= now() - make_interval(secs => sqlc.arg(cooldown_seconds)::int))
RETURNING last_placed_at::timestamptz AS placed_at;

-- name: GetLastPlacedAt :one
SELECT last_placed_at FROM space_members
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id);

-- name: InsertPixel :one
INSERT INTO pixels (space_id, canvas_id, x, y, color, user_id, source)
VALUES (sqlc.arg(space_id), sqlc.arg(canvas_id), sqlc.arg(x), sqlc.arg(y), sqlc.arg(color), sqlc.arg(user_id), sqlc.arg(source))
RETURNING id, placed_at;

-- name: UpsertCanvasPixel :exec
INSERT INTO canvas_pixels (canvas_id, x, y, space_id, color, pixel_id)
VALUES (sqlc.arg(canvas_id), sqlc.arg(x), sqlc.arg(y), sqlc.arg(space_id), sqlc.arg(color), sqlc.arg(pixel_id))
ON CONFLICT (canvas_id, x, y) DO UPDATE SET color = EXCLUDED.color, pixel_id = EXCLUDED.pixel_id;
