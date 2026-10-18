-- A chat account linked to a member and the canvas its /place draws on. One chat account,
-- one user; linking again replaces it. Looked up by the chat account, so no RLS (like sessions).

-- +goose Up
CREATE TABLE bot_links (
    platform         text NOT NULL CHECK (platform IN ('telegram')),
    platform_user_id text NOT NULL,
    user_id          uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    space_id         uuid NOT NULL,
    canvas_id        uuid NOT NULL,
    linked_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, platform_user_id),
    FOREIGN KEY (canvas_id, space_id) REFERENCES canvases (id, space_id) ON DELETE CASCADE
);
CREATE INDEX bot_links_user_id_idx ON bot_links (user_id);

-- +goose Down
DROP TABLE bot_links;
