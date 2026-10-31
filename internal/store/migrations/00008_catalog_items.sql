-- +goose Up
CREATE TABLE IF NOT EXISTS catalog_items (
  id          bigserial PRIMARY KEY,
  catalog_id  int NOT NULL REFERENCES catalogs(id),
  page        int NOT NULL,
  sku         text NOT NULL,
  sku_base    text,
  line        text NOT NULL,
  price_cents bigint,
  x int, y int,
  UNIQUE (catalog_id, page, sku, y)
);
CREATE INDEX IF NOT EXISTS catalog_items_sku ON catalog_items (sku);
ALTER TABLE product_families ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'site';
ALTER TABLE products ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'site';
ALTER TABLE products ADD COLUMN IF NOT EXISTS catalog_id int REFERENCES catalogs(id);
ALTER TABLE products ADD COLUMN IF NOT EXISTS catalog_page int;
-- +goose Down
DROP TABLE IF EXISTS catalog_items;
