-- +goose Up
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS sku_base text;
UPDATE catalog_crops SET sku_base = NULLIF(regexp_replace(regexp_replace(upper(sku), '[.-]X{2,4}(?=[.-]|$)', '', 'g'), '[.-]+$', ''), '') WHERE sku IS NOT NULL;
CREATE INDEX IF NOT EXISTS catalog_crops_base ON catalog_crops (sku_base);
CREATE INDEX IF NOT EXISTS products_sku_prefix ON products (sku text_pattern_ops);
CREATE INDEX IF NOT EXISTS product_families_key_prefix ON product_families (family_key text_pattern_ops);

CREATE TABLE IF NOT EXISTS family_crops (
  family_id bigint NOT NULL REFERENCES product_families(id) ON DELETE CASCADE,
  crop_id   bigint NOT NULL REFERENCES catalog_crops(id) ON DELETE CASCADE,
  how       text NOT NULL,   -- exact | family | prefix
  PRIMARY KEY (family_id, crop_id)
);
CREATE INDEX IF NOT EXISTS family_crops_crop ON family_crops (crop_id);

-- +goose Down
DROP TABLE IF EXISTS family_crops;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS sku_base;
DROP INDEX IF EXISTS products_sku_prefix, product_families_key_prefix;
