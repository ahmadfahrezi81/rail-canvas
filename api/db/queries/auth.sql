-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES (sqlc.arg(email), sqlc.arg(password_hash), sqlc.arg(display_name))
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = sqlc.arg(email);

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateSession :exec
INSERT INTO sessions (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: GetSessionUser :one
SELECT u.* FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = sqlc.arg(token_hash)
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND u.status = 'active';

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now()
WHERE token_hash = sqlc.arg(token_hash) AND revoked_at IS NULL;

-- name: CreateInvite :one
INSERT INTO invites (space_id, code_hash, created_by, expires_at)
VALUES (sqlc.arg(space_id), sqlc.arg(code_hash), sqlc.arg(created_by), sqlc.arg(expires_at))
RETURNING expires_at;

-- name: RedeemInvite :one
-- Atomic: a concurrent redeem blocks on the row, then sees used_at set and matches nothing.
UPDATE invites SET used_by = sqlc.arg(user_id), used_at = now()
WHERE code_hash = sqlc.arg(code_hash) AND used_at IS NULL AND expires_at > now()
RETURNING space_id;
