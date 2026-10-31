-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector') THEN
    CREATE EXTENSION IF NOT EXISTS vector;
    ALTER TABLE product_images ADD COLUMN IF NOT EXISTS embedding vector(768);
    ALTER TABLE catalog_pages  ADD COLUMN IF NOT EXISTS embedding vector(768);
    ALTER TABLE product_families ADD COLUMN IF NOT EXISTS embedding vector(768);
  ELSE
    RAISE NOTICE 'pgvector not available; skipping embedding columns';
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE product_images DROP COLUMN IF EXISTS embedding;
ALTER TABLE catalog_pages DROP COLUMN IF EXISTS embedding;
ALTER TABLE product_families DROP COLUMN IF EXISTS embedding;
