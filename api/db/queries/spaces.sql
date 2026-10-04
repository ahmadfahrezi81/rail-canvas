-- name: CreateSpace :one
INSERT INTO spaces (name, slug) VALUES (sqlc.arg(name), sqlc.arg(slug))
RETURNING *;

-- name: AddMember :exec
INSERT INTO space_members (space_id, user_id, role)
VALUES (sqlc.arg(space_id), sqlc.arg(user_id), sqlc.arg(role))
ON CONFLICT (space_id, user_id) DO NOTHING;

-- name: GetMemberRole :one
SELECT role FROM space_members
WHERE space_id = sqlc.arg(space_id) AND user_id = sqlc.arg(user_id);

-- name: ListUserSpaces :many
SELECT s.id, s.name, s.slug, m.role
FROM space_members m
JOIN spaces s ON s.id = m.space_id
WHERE m.user_id = sqlc.arg(user_id)
ORDER BY m.joined_at;
