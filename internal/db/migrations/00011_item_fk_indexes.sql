-- +goose Up
-- ON DELETE SET NULL cascades from items look rows up by item_id; without these a bulk delete
-- of feed items re-scanned documents and family_media once per item.
CREATE INDEX documents_item ON documents (item_id) WHERE item_id IS NOT NULL;
CREATE INDEX family_media_item ON family_media (item_id) WHERE item_id IS NOT NULL;
-- +goose Down
DROP INDEX IF EXISTS documents_item;
DROP INDEX IF EXISTS family_media_item;
