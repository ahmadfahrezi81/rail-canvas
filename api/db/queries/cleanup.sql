-- The nightly cleanup (cli cleanup). Each rule has a count, for the dry run, and a delete.
-- sessions, ws_tickets and invites have no RLS: these run without a tenant.

-- name: CountOldSessions :one
SELECT count(*) FROM sessions WHERE expires_at < sqlc.arg(before) OR revoked_at < sqlc.arg(before);

-- name: DeleteOldSessions :execrows
DELETE FROM sessions WHERE expires_at < sqlc.arg(before) OR revoked_at < sqlc.arg(before);

-- name: CountOldWSTickets :one
SELECT count(*) FROM ws_tickets WHERE expires_at < sqlc.arg(before);

-- name: DeleteOldWSTickets :execrows
DELETE FROM ws_tickets WHERE expires_at < sqlc.arg(before);

-- name: CountOldInvites :one
SELECT count(*) FROM invites WHERE expires_at < sqlc.arg(before);

-- name: DeleteOldInvites :execrows
DELETE FROM invites WHERE expires_at < sqlc.arg(before);

-- name: CountOldPixels :one
SELECT count(*) FROM pixels
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id) AND placed_at < sqlc.arg(before);

-- name: DeleteOldPixels :execrows
-- One batch, oldest first, along the (canvas_id, placed_at) index.
DELETE FROM pixels WHERE id IN (
    SELECT o.id FROM pixels o
    WHERE o.canvas_id = sqlc.arg(canvas_id) AND o.space_id = sqlc.arg(space_id) AND o.placed_at < sqlc.arg(before)
    ORDER BY o.placed_at
    LIMIT sqlc.arg(batch)
);

-- name: ListOldTimelapses :many
-- Ready or failed ones beyond the canvas's newest kept_rows, and failed ones older than failed_before.
-- A pending one is never listed.
SELECT id, object_key FROM (
    SELECT id, object_key, status, created_at, row_number() OVER (ORDER BY created_at DESC) AS rn
    FROM timelapses
    WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
) t
WHERE status <> 'pending'
  AND (rn > sqlc.arg(kept_rows)::bigint OR (status = 'failed' AND created_at < sqlc.arg(failed_before)));

-- name: ListStalePendingTemplates :many
-- Uploads that never completed: the signed URL lasted 5 minutes.
SELECT id, object_key FROM templates
WHERE space_id = sqlc.arg(space_id) AND status = 'pending' AND created_at < sqlc.arg(before);
