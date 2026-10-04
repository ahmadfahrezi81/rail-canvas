-- name: SetTenant :exec
SELECT set_config('app.tenant_id', sqlc.arg(space_id)::text, true);

-- name: CreateCanvas :one
INSERT INTO canvases (space_id, name, created_by, cooldown_seconds)
VALUES (sqlc.arg(space_id), sqlc.arg(name), sqlc.arg(created_by), sqlc.arg(cooldown_seconds))
RETURNING *;

-- name: ListCanvases :many
SELECT * FROM canvases
WHERE space_id = sqlc.arg(space_id)
ORDER BY id DESC;

-- name: GetCanvas :one
SELECT * FROM canvases
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id);

-- name: ListCanvasPixels :many
SELECT x, y, color FROM canvas_pixels
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id);
