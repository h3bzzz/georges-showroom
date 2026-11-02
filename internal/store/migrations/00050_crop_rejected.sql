-- +goose Up
-- A crop a human has refused as a card picture (the image-QA loop: a page screenshot shows a
-- wrong, ugly or mislabelled picture, it is rejected by path and the family re-picked).
-- Rejection outranks every model score; it is never cleared by a reclassify.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS rejected boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS catalog_crops_rejected ON catalog_crops (id) WHERE rejected;
-- +goose Down
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS rejected;
