-- +goose Up
CREATE TABLE catalog_skus (
  catalog_id int NOT NULL REFERENCES catalogs(id),
  page       int NOT NULL,
  sku        text NOT NULL,
  x int, y int, w int, h int,
  conf       real,
  PRIMARY KEY (catalog_id, page, sku, x, y)
);
CREATE INDEX catalog_skus_sku ON catalog_skus (sku);
CREATE INDEX catalog_skus_sku_trgm ON catalog_skus USING gin (sku gin_trgm_ops);

CREATE TABLE catalog_crops (
  id         bigserial PRIMARY KEY,
  catalog_id int NOT NULL REFERENCES catalogs(id),
  page       int NOT NULL,
  x int, y int, w int, h int,
  path       text NOT NULL,
  sku        text,
  score      real,
  UNIQUE (catalog_id, page, x, y, w, h)
);
CREATE INDEX catalog_crops_sku ON catalog_crops (sku);

ALTER TABLE catalog_pages ADD COLUMN IF NOT EXISTS ocr_at timestamptz;
ALTER TABLE catalog_pages ADD COLUMN IF NOT EXISTS extracted_at timestamptz;

-- +goose Down
DROP TABLE IF EXISTS catalog_crops, catalog_skus;
ALTER TABLE catalog_pages DROP COLUMN IF EXISTS ocr_at, DROP COLUMN IF EXISTS extracted_at;
