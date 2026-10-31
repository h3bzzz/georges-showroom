-- +goose Up
CREATE OR REPLACE FUNCTION sku_norm(t text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT upper(regexp_replace(COALESCE(t,''), '[^A-Za-z0-9]', '', 'g'))
$$;
CREATE INDEX IF NOT EXISTS products_sku_norm ON products (sku_norm(sku) text_pattern_ops);
CREATE INDEX IF NOT EXISTS product_families_key_norm ON product_families (sku_norm(family_key));
CREATE INDEX IF NOT EXISTS catalog_crops_base_norm ON catalog_crops (sku_norm(sku_base));
CREATE INDEX IF NOT EXISTS catalog_skus_base_norm ON catalog_skus (sku_norm(sku_base));
-- +goose Down
DROP FUNCTION IF EXISTS sku_norm(text);
