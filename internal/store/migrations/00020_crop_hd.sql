-- +goose Up
-- Super-resolved (Real-ESRGAN 4x) copies of small catalog crops. hd_w = -1 marks a failed attempt.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS hd_path text;
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS hd_w int;
CREATE INDEX IF NOT EXISTS family_best_photo_path ON family_best (photo_path);
CREATE INDEX IF NOT EXISTS family_best_drawing_path ON family_best (drawing_path);
CREATE INDEX IF NOT EXISTS catalog_crops_path ON catalog_crops (path);

-- +goose Down
DROP INDEX IF EXISTS catalog_crops_path;
DROP INDEX IF EXISTS family_best_drawing_path;
DROP INDEX IF EXISTS family_best_photo_path;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS hd_w;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS hd_path;
