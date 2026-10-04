-- Append-only placement history. canvas_pixels.pixel_id points here by value
-- only (no foreign key): retention cleanup must be free to delete old rows.

-- +goose Up
CREATE TABLE pixels (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    space_id  uuid NOT NULL,
    canvas_id uuid NOT NULL,
    x         smallint NOT NULL CHECK (x BETWEEN 0 AND 255),
    y         smallint NOT NULL CHECK (y BETWEEN 0 AND 255),
    color     smallint NOT NULL CHECK (color BETWEEN 0 AND 255),
    user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
    source    text NOT NULL DEFAULT 'web' CHECK (source IN ('web', 'bot')),
    placed_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
CREATE INDEX pixels_canvas_id_placed_at_idx ON pixels (canvas_id, placed_at);

ALTER TABLE pixels ENABLE ROW LEVEL SECURITY;
ALTER TABLE pixels FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON pixels
    USING (space_id = current_tenant_id())
    WITH CHECK (space_id = current_tenant_id());

-- +goose Down
DROP TABLE pixels;
