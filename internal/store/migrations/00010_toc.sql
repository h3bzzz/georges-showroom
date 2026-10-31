-- +goose Up
CREATE TABLE IF NOT EXISTS catalog_toc (
  catalog_id int NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
  page       int NOT NULL,
  title      text NOT NULL,
  level      int NOT NULL DEFAULT 1,
  source     text NOT NULL DEFAULT 'ocr',
  PRIMARY KEY (catalog_id, page, title)
);
-- +goose Down
DROP TABLE IF EXISTS catalog_toc;
