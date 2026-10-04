-- +goose Up
CREATE TABLE canvases (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    space_id         uuid NOT NULL REFERENCES spaces (id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    -- Fixed at 256 for now; the board encoding assumes it.
    width            smallint NOT NULL DEFAULT 256 CHECK (width = 256),
    height           smallint NOT NULL DEFAULT 256 CHECK (height = 256),
    palette          text NOT NULL DEFAULT 'classic16',
    cooldown_seconds integer NOT NULL DEFAULT 300 CHECK (cooldown_seconds >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- Target for canvas_pixels' composite key, so its space_id cannot drift.
    UNIQUE (id, space_id)
);
CREATE INDEX canvases_space_id_idx ON canvases (space_id);

-- The current board. Sparse: a row only for a painted cell.
CREATE TABLE canvas_pixels (
    canvas_id uuid NOT NULL,
    x         smallint NOT NULL CHECK (x BETWEEN 0 AND 255),
    y         smallint NOT NULL CHECK (y BETWEEN 0 AND 255),
    space_id  uuid NOT NULL,
    color     smallint NOT NULL CHECK (color BETWEEN 0 AND 255),
    -- Version for the Redis compare-and-set. Foreign key to pixels in Step 6.
    pixel_id  bigint NOT NULL,
    PRIMARY KEY (canvas_id, x, y),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);

ALTER TABLE canvases ENABLE ROW LEVEL SECURITY;
ALTER TABLE canvases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON canvases
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

ALTER TABLE canvas_pixels ENABLE ROW LEVEL SECURITY;
ALTER TABLE canvas_pixels FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON canvas_pixels
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

-- +goose Down
DROP TABLE canvas_pixels;
DROP TABLE canvases;
