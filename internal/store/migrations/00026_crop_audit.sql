-- +goose Up
-- Crop audit: pixel statistics + OCR word density so tables, text blocks, blanks and line art
-- can be kept out of the photo pool, and better photos ranked first.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS ink real;      -- non-white share of pixels
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS midtone real;  -- share of non-white pixels that are mid-tones (photos) vs black lines
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS sat real;      -- mean saturation of non-white pixels
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS words int;     -- OCR words inside the crop
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS textarea real; -- share of the crop covered by OCR words
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS quality real;  -- 0..1 photo quality for ranking
CREATE INDEX IF NOT EXISTS catalog_crops_kind ON catalog_crops (kind);
-- +goose Down
SELECT 1;
