-- Chat bots (Step 15). link_codes and bot_links have no RLS: they are looked up by a code
-- or a chat account before the space is known.

-- name: CreateLinkCode :one
INSERT INTO link_codes (code_hash, user_id, space_id, canvas_id, expires_at)
VALUES (sqlc.arg(code_hash), sqlc.arg(user_id), sqlc.arg(space_id), sqlc.arg(canvas_id), sqlc.arg(expires_at))
RETURNING expires_at;

-- name: RedeemLinkCode :one
-- Atomic single use, like invites, and only for a still-active user.
UPDATE link_codes c SET used_at = now()
FROM users u
WHERE c.code_hash = sqlc.arg(code_hash) AND c.used_at IS NULL AND c.expires_at > now()
  AND u.id = c.user_id AND u.status = 'active'
RETURNING c.user_id, c.space_id, c.canvas_id, u.display_name;

-- name: UpsertBotLink :exec
INSERT INTO bot_links (platform, platform_user_id, user_id, space_id, canvas_id)
VALUES (sqlc.arg(platform), sqlc.arg(platform_user_id), sqlc.arg(user_id), sqlc.arg(space_id), sqlc.arg(canvas_id))
ON CONFLICT (platform, platform_user_id) DO UPDATE
SET user_id = EXCLUDED.user_id, space_id = EXCLUDED.space_id, canvas_id = EXCLUDED.canvas_id, linked_at = now();

-- name: GetBotLink :one
SELECT user_id, space_id, canvas_id FROM bot_links
WHERE platform = sqlc.arg(platform) AND platform_user_id = sqlc.arg(platform_user_id);
