-- +goose Up
-- Where each SKU is printed in the catalogs: places a family on a page (series-opener
-- fallback, "see page N" links) and lets a crop find the SKU rows beside it.
CREATE TABLE catalog_skus (
  catalog_id int NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
  page       int NOT NULL,
  sku_norm   text NOT NULL,
  x int, y int, w int, h int,
  PRIMARY KEY (catalog_id, page, sku_norm, x, y)
);
CREATE INDEX catalog_skus_sku ON catalog_skus (sku_norm);
-- +goose Down
DROP TABLE IF EXISTS catalog_skus;
