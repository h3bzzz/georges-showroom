-- +goose Up
ALTER TABLE product_families ADD COLUMN IF NOT EXISTS webimg_tried_at timestamptz;
ALTER TABLE product_images DROP CONSTRAINT IF EXISTS product_images_source_check;
ALTER TABLE product_images ADD CONSTRAINT product_images_source_check CHECK (source IN ('site','catalog','spec','upload','web'));
ALTER TABLE product_images ADD COLUMN IF NOT EXISTS page_url text;
-- +goose Down
ALTER TABLE product_families DROP COLUMN IF EXISTS webimg_tried_at;
