-- Single-use WebSocket login tickets. Only a SHA-256 is stored. Moves to Redis in Step 11.

-- +goose Up
CREATE TABLE ws_tickets (
    ticket_hash bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz
);

-- +goose Down
DROP TABLE ws_tickets;
