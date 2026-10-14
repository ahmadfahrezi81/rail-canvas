-- name: CreateTemplate :one
INSERT INTO templates (id, space_id, canvas_id, uploaded_by, object_key, x, y, bytes)
VALUES (sqlc.arg(id), sqlc.arg(space_id), sqlc.arg(canvas_id), sqlc.arg(uploaded_by), sqlc.arg(object_key),
        sqlc.arg(x), sqlc.arg(y), sqlc.arg(bytes))
RETURNING created_at;

-- name: CountReadyTemplates :one
SELECT count(*) FROM templates
WHERE space_id = sqlc.arg(space_id) AND canvas_id = sqlc.arg(canvas_id) AND status = 'ready';

-- name: GetTemplate :one
SELECT * FROM templates
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id) AND canvas_id = sqlc.arg(canvas_id);

-- name: MarkTemplateReady :one
-- Only a pending template: completing twice matches nothing.
UPDATE templates SET status = 'ready', width = sqlc.arg(width), height = sqlc.arg(height)
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id) AND status = 'pending'
RETURNING *;

-- name: MarkTemplateRejected :execrows
UPDATE templates SET status = 'rejected', reject_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id) AND status = 'pending';

-- name: ListReadyTemplates :many
SELECT t.*, u.display_name AS uploaded_by_name
FROM templates t LEFT JOIN users u ON u.id = t.uploaded_by
WHERE t.space_id = sqlc.arg(space_id) AND t.canvas_id = sqlc.arg(canvas_id) AND t.status = 'ready'
ORDER BY t.created_at DESC;

-- name: DeleteTemplate :exec
DELETE FROM templates WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id);
