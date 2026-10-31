-- +goose Up
ALTER TABLE product_families ADD COLUMN IF NOT EXISTS room text;
CREATE INDEX IF NOT EXISTS product_families_room ON product_families (room);
CREATE INDEX IF NOT EXISTS product_families_cat ON product_families USING gin (category_path);
CREATE INDEX IF NOT EXISTS products_finish ON products (finish);
-- +goose Down
ALTER TABLE product_families DROP COLUMN IF EXISTS room;
