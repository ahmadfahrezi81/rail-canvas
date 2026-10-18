-- One-time codes that tie a chat account (Telegram) to a member, made from a canvas page.
-- Only a SHA-256 of the code is stored. Looked up by the code before the space is known,
-- so no RLS (like invites); queries are scoped in Go.

-- +goose Up
CREATE TABLE link_codes (
    code_hash  bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    space_id   uuid NOT NULL,
    canvas_id  uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE link_codes;
