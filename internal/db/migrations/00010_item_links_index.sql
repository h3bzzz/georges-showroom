-- +goose Up
-- Product pages look up each SKU's own picture through asset_links.item_id; a family with
-- hundreds of models (feed hardware) scanned the whole table once per model (5 s a page).
CREATE INDEX asset_links_item ON asset_links (item_id) WHERE item_id IS NOT NULL;
-- "related" on a product page filters search_docs by category alone
CREATE INDEX search_docs_category ON search_docs (category, grade);
-- +goose Down
DROP INDEX IF EXISTS asset_links_item;
DROP INDEX IF EXISTS search_docs_category;
