-- +goose Up
-- dup_of points a crop at the larger copy of the same shot printed elsewhere in the brand's
-- books (the same collection photograph opens three price lists), found by CLIP cosine on the
-- stored embeddings (catalogs dedupe). Galleries and the upscaler skip duplicates.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS dup_of bigint;
CREATE INDEX IF NOT EXISTS catalog_crops_dup_of ON catalog_crops (dup_of) WHERE dup_of IS NOT NULL;
-- +goose Down
DROP INDEX IF EXISTS catalog_crops_dup_of;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS dup_of;
