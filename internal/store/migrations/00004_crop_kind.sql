-- +goose Up
ALTER TABLE catalog_crops ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'photo';
-- +goose Down
ALTER TABLE catalog_crops DROP COLUMN IF EXISTS kind;
