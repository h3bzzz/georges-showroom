-- +goose Up
-- The company price file (FTP) is the authoritative price source: what it says is the
-- published price, cost is for trade mode only, and the row remembers where its price came
-- from so catalog OCR never overwrites a feed price.
ALTER TABLE items
  ADD COLUMN cost_cents       bigint,
  ADD COLUMN price_source     text,          -- pricefile | site | catalog
  ADD COLUMN price_updated_at timestamptz,
  ADD COLUMN discontinued     boolean NOT NULL DEFAULT false;
CREATE INDEX items_brand_sku_norm ON items (brand_id, sku_norm);
-- +goose Down
DROP INDEX IF EXISTS items_brand_sku_norm;
ALTER TABLE items DROP COLUMN cost_cents, DROP COLUMN price_source, DROP COLUMN price_updated_at, DROP COLUMN discontinued;
