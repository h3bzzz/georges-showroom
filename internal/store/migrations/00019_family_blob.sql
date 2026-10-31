-- +goose Up
-- One trigram-indexed text blob per family (title, description, keys, collection, brand, type,
-- categories, every SKU incl. normalized form, every finish). Browse/search match each token
-- against it instead of six ILIKEs plus a products subquery (~150 ms per token before).
ALTER TABLE family_best ADD COLUMN IF NOT EXISTS blob text;
CREATE INDEX IF NOT EXISTS family_best_blob_trgm ON family_best USING gin (blob gin_trgm_ops);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION refresh_family_blob() RETURNS void LANGUAGE sql AS $$
  UPDATE family_best fb SET blob = sub.blob
  FROM (
    SELECT f.id, concat_ws(' ', f.title, f.description, f.family_key, f.key_n, f.collection, b.name,
      f.product_type, array_to_string(f.category_path, ' '),
      (SELECT string_agg(DISTINCT p.sku || ' ' || COALESCE(p.sku_n, ''), ' ') FROM products p WHERE p.family_id = f.id),
      (SELECT string_agg(DISTINCT p.finish, ' ') FROM products p WHERE p.family_id = f.id)) AS blob
    FROM product_families f JOIN brands b ON b.id = f.brand_id) sub
  WHERE fb.family_id = sub.id AND fb.blob IS DISTINCT FROM sub.blob;
$$;
-- +goose StatementEnd
SELECT refresh_family_blob();

-- +goose Down
DROP FUNCTION IF EXISTS refresh_family_blob();
DROP INDEX IF EXISTS family_best_blob_trgm;
ALTER TABLE family_best DROP COLUMN IF EXISTS blob;
