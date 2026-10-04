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

-- name: CreateWSTicket :exec
INSERT INTO ws_tickets (ticket_hash, user_id, expires_at)
VALUES (sqlc.arg(ticket_hash), sqlc.arg(user_id), sqlc.arg(expires_at));

-- name: RedeemWSTicket :one
-- Atomic single use, and only for a still-active user.
UPDATE ws_tickets t SET used_at = now()
FROM users u
WHERE t.ticket_hash = sqlc.arg(ticket_hash) AND t.used_at IS NULL AND t.expires_at > now()
  AND u.id = t.user_id AND u.status = 'active'
RETURNING u.id, u.email, u.display_name;
