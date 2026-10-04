-- The tenant. No RLS: it is read before a tenant is known.

-- +goose Up
CREATE TABLE spaces (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    slug       text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{1,32}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE spaces;
