-- Nullable: canvases made before login have no creator.

-- +goose Up
ALTER TABLE canvases ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE canvases DROP COLUMN created_by;
