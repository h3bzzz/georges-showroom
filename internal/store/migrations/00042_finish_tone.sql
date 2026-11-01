-- +goose Up
-- vtone is the CLIP verdict on the colour of the metal in a photo crop (silver | gold | bronze |
-- black | white | copper | gray | other), scored from the stored embedding by `catalogs vision
-- --tone`. A finish query ranks families whose pictures carry the asked tone first and swaps
-- the card photo for a crop in that tone. product_images rows get the tone of the SKU's finish.
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS vtone text;
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS vtonescore real;
ALTER TABLE product_images ADD COLUMN IF NOT EXISTS vtone text;
CREATE INDEX IF NOT EXISTS catalog_crops_hd_path ON catalog_crops (hd_path) WHERE hd_path IS NOT NULL;
CREATE INDEX IF NOT EXISTS product_images_path ON product_images (path) WHERE path IS NOT NULL;
CREATE INDEX IF NOT EXISTS products_finish_family ON products (finish, family_id) WHERE finish IS NOT NULL;
-- +goose Down
DROP INDEX IF EXISTS products_finish_family;
DROP INDEX IF EXISTS catalog_crops_hd_path;
DROP INDEX IF EXISTS product_images_path;
ALTER TABLE product_images DROP COLUMN IF EXISTS vtone;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS vtonescore;
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS vtone;
