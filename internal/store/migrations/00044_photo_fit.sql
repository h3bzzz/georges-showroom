-- +goose Up
-- Title-to-picture fit and scene detection (internal/store/fit.go, `catalogs vision --fit/--scene`).
-- vscene: zero-shot probability that a photo crop is a room interior / mood shot rather than a
-- product; crop_fit: cosine between the family's name ("a photo of a Toilet Brush Set") and a
-- candidate crop's CLIP embedding, for every crop that could stand as the family's card
-- (linked crops and series openers). RefreshFamilyBest reads both.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS vscene real;
CREATE TABLE IF NOT EXISTS crop_fit (
  family_id bigint NOT NULL REFERENCES product_families(id) ON DELETE CASCADE,
  crop_id   bigint NOT NULL REFERENCES catalog_crops(id) ON DELETE CASCADE,
  fit       real NOT NULL,
  PRIMARY KEY (family_id, crop_id)
);
ALTER TABLE family_best ADD COLUMN IF NOT EXISTS photo_fit real;
-- +goose Down
DROP TABLE IF EXISTS crop_fit;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS vscene;
ALTER TABLE family_best DROP COLUMN IF EXISTS photo_fit;
