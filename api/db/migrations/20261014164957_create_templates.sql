-- Reference images overlaid on a canvas. The file lives in the bucket at object_key.

-- +goose Up
CREATE TABLE templates (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id      uuid NOT NULL,
    canvas_id     uuid NOT NULL,
    uploaded_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    object_key    text NOT NULL UNIQUE,
    -- Filled by the worker in Step 12.
    thumb_key     text,
    x             smallint NOT NULL CHECK (x BETWEEN 0 AND 255),
    y             smallint NOT NULL CHECK (y BETWEEN 0 AND 255),
    -- Known only once the upload is checked.
    width         smallint CHECK (width BETWEEN 1 AND 256),
    height        smallint CHECK (height BETWEEN 1 AND 256),
    bytes         integer NOT NULL CHECK (bytes BETWEEN 1 AND 1048576),
    status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'rejected')),
    reject_reason text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
CREATE INDEX templates_canvas_id_status_idx ON templates (canvas_id, status);

ALTER TABLE templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE templates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON templates
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

-- +goose Down
DROP TABLE templates;
