-- Animated GIFs of a canvas, built by the worker from its hourly snapshots.

-- +goose Up
CREATE TABLE timelapses (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id     uuid NOT NULL,
    canvas_id    uuid NOT NULL,
    requested_by uuid REFERENCES users (id) ON DELETE SET NULL,
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    -- Set when ready.
    object_key   text UNIQUE,
    frames       integer,
    -- Set when failed.
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
-- At most one pending timelapse per canvas.
CREATE UNIQUE INDEX timelapses_one_pending ON timelapses (canvas_id) WHERE status = 'pending';
CREATE INDEX timelapses_canvas_id_created_at_idx ON timelapses (canvas_id, created_at DESC);

ALTER TABLE timelapses ENABLE ROW LEVEL SECURITY;
ALTER TABLE timelapses FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON timelapses
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

-- +goose Down
DROP TABLE timelapses;
