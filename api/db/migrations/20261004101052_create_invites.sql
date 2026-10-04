-- Single-use codes to join a space. Only a SHA-256 of the code is stored.

-- +goose Up
CREATE TABLE invites (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id   uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    code_hash  bytea NOT NULL UNIQUE,
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    used_at    timestamptz
);
CREATE INDEX invites_space_id_idx ON invites (space_id);

-- +goose Down
DROP TABLE invites;
