-- +goose Up
-- family keys scraped from ?model= links carry the brand code ("CAL-3501-2"); strip it
UPDATE product_families f SET family_key = substr(f.family_key, length(b.code)+2)
FROM brands b
WHERE b.id=f.brand_id AND COALESCE(b.code,'')<>'' AND f.family_key LIKE b.code||'-%'
  AND NOT EXISTS (SELECT 1 FROM product_families g WHERE g.brand_id=f.brand_id AND g.family_key=substr(f.family_key, length(b.code)+2));

ALTER TABLE catalog_skus ADD COLUMN IF NOT EXISTS sku_base text;
UPDATE catalog_skus SET sku_base = NULLIF(regexp_replace(regexp_replace(upper(sku), '[.-]X{2,4}([.-].*)?$', ''), '[.-]+$', ''), '');
CREATE INDEX IF NOT EXISTS catalog_skus_base ON catalog_skus (sku_base);
CREATE INDEX IF NOT EXISTS catalog_crops_page ON catalog_crops (catalog_id, page);
-- +goose Down
ALTER TABLE catalog_skus DROP COLUMN IF EXISTS sku_base;
