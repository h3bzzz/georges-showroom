-- +goose Up
-- one row per family with its best images, refreshed by the linker: no per-row subqueries at read time
CREATE TABLE IF NOT EXISTS family_best (
  family_id    bigint PRIMARY KEY REFERENCES product_families(id) ON DELETE CASCADE,
  photo_path   text, photo_w int,
  drawing_path text,
  web_path     text,
  page_catalog int, page_no int,
  updated_at   timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE IF EXISTS family_best;
