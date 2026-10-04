-- No RLS: a user can belong to several spaces; scoped in Go.

-- +goose Up
CREATE TABLE space_members (
    space_id       uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role           text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    last_placed_at timestamptz, -- the Postgres cooldown (Step 6)
    joined_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id)
);
CREATE INDEX space_members_user_id_idx ON space_members (user_id);

-- +goose Down
DROP TABLE space_members;
