-- Hourly PNG of a canvas. The file lives in the bucket at object_key.

-- +goose Up
CREATE TABLE snapshots (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id      uuid NOT NULL,
    canvas_id     uuid NOT NULL,
    object_key    text NOT NULL UNIQUE,
    -- Newest pixel in the picture: an unchanged canvas is not snapshotted again.
    last_pixel_id bigint NOT NULL,
    -- The hour, truncated: two API versions running during a deploy make one row.
    taken_at      timestamptz NOT NULL,
    UNIQUE (canvas_id, taken_at),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);

ALTER TABLE snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON snapshots
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

-- +goose Down
DROP TABLE snapshots;
