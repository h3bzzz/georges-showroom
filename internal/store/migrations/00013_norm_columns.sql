-- +goose Up
ALTER TABLE products ADD COLUMN IF NOT EXISTS sku_n text GENERATED ALWAYS AS (sku_norm(sku)) STORED;
ALTER TABLE product_families ADD COLUMN IF NOT EXISTS key_n text GENERATED ALWAYS AS (sku_norm(family_key)) STORED;
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS base_n text GENERATED ALWAYS AS (sku_norm(sku_base)) STORED;
ALTER TABLE catalog_skus ADD COLUMN IF NOT EXISTS base_n text GENERATED ALWAYS AS (sku_norm(sku_base)) STORED;
CREATE INDEX IF NOT EXISTS products_sku_n ON products (sku_n text_pattern_ops);
CREATE INDEX IF NOT EXISTS product_families_key_n ON product_families (key_n);
CREATE INDEX IF NOT EXISTS catalog_crops_base_n ON catalog_crops (base_n);
CREATE INDEX IF NOT EXISTS catalog_skus_base_n ON catalog_skus (base_n);
DROP INDEX IF EXISTS products_sku_norm, product_families_key_norm, catalog_crops_base_norm, catalog_skus_base_norm;
ALTER TABLE brands ADD COLUMN IF NOT EXISTS price_multiplier numeric(4,3) NOT NULL DEFAULT 0.750;
-- +goose Down
ALTER TABLE products DROP COLUMN IF EXISTS sku_n;
ALTER TABLE product_families DROP COLUMN IF EXISTS key_n;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS base_n;
ALTER TABLE catalog_skus DROP COLUMN IF EXISTS base_n;
ALTER TABLE brands DROP COLUMN IF EXISTS price_multiplier;
