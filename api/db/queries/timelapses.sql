-- name: FailStaleTimelapses :exec
-- A pending one this old lost its job (Redis lost its data): it must not block the canvas.
UPDATE timelapses SET status = 'failed', error = 'timed out', finished_at = now()
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
  AND status = 'pending' AND created_at < now() - make_interval(secs => sqlc.arg(older_than_seconds)::int);

-- name: CreateTimelapse :one
INSERT INTO timelapses (space_id, canvas_id, requested_by)
VALUES (sqlc.arg(space_id), sqlc.arg(canvas_id), sqlc.arg(requested_by))
RETURNING id;

-- name: GetTimelapse :one
SELECT * FROM timelapses WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id);

-- name: MarkTimelapseReady :execrows
UPDATE timelapses SET status = 'ready', object_key = sqlc.arg(object_key), frames = sqlc.arg(frames), finished_at = now()
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id) AND status = 'pending';

-- name: MarkTimelapseFailed :exec
UPDATE timelapses SET status = 'failed', error = sqlc.arg(reason), finished_at = now()
WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id) AND status = 'pending';

-- name: ListTimelapses :many
SELECT * FROM timelapses
WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
ORDER BY created_at DESC
LIMIT 10;

-- name: ListSnapshotKeysForTimelapse :many
-- The newest max_frames snapshots, oldest first.
SELECT object_key FROM (
    SELECT object_key, taken_at FROM snapshots
    WHERE canvas_id = sqlc.arg(canvas_id) AND space_id = sqlc.arg(space_id)
    ORDER BY taken_at DESC
    LIMIT sqlc.arg(max_frames)
) newest ORDER BY taken_at;

-- name: DeleteTimelapse :exec
DELETE FROM timelapses WHERE id = sqlc.arg(id) AND space_id = sqlc.arg(space_id);
