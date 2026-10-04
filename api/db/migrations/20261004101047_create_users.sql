-- No RLS: read before a space is known (login).

-- +goose Up
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    email         text NOT NULL UNIQUE CHECK (email = lower(email) AND length(email) BETWEEN 3 AND 254),
    password_hash text NOT NULL,
    display_name  text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 32),
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
