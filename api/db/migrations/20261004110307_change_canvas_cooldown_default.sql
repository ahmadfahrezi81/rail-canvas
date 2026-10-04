-- 10 seconds suits a small group. Existing canvases keep their value.

-- +goose Up
ALTER TABLE canvases ALTER COLUMN cooldown_seconds SET DEFAULT 10;

-- +goose Down
ALTER TABLE canvases ALTER COLUMN cooldown_seconds SET DEFAULT 300;
