-- +goose Up
CREATE TABLE IF NOT EXISTS covers (
  kind       text NOT NULL,     -- room | category | home
  key        text NOT NULL,     -- room slug, or "<room>|<category>"
  path       text NOT NULL,     -- relative to DATA_DIR
  catalog_id int,
  page       int,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, key)
);
-- +goose Down
DROP TABLE IF EXISTS covers;
