-- +goose Up
-- One precomputed sort key per browse flavour so a page of 48 cards is an index walk, not a
-- sort of 600k rows: ord = tier, picture bucket, brand round-robin rank, popularity; ord_cat
-- uses the per-category round-robin. Filled here for existing rows and by `index build`.
ALTER TABLE search_docs ADD COLUMN ord bigint NOT NULL DEFAULT 0, ADD COLUMN ord_cat bigint NOT NULL DEFAULT 0;
UPDATE search_docs SET
  ord     = ((tier::bigint * 4 + CASE WHEN grade <= 1 THEN 0 ELSE grade END) * 1000000 + LEAST(rr_room, 999999)) * 100000 + LEAST(99999, GREATEST(0, 50000 - round(pop * 1000)))::bigint,
  ord_cat = ((tier::bigint * 4 + CASE WHEN grade <= 1 THEN 0 ELSE grade END) * 1000000 + LEAST(rr_cat, 999999)) * 100000 + LEAST(99999, GREATEST(0, 50000 - round(pop * 1000)))::bigint;
CREATE INDEX search_docs_ord ON search_docs (ord, family_id);
CREATE INDEX search_docs_room_ord ON search_docs (room, ord, family_id);
CREATE INDEX search_docs_cat_ord ON search_docs (category, ord_cat, family_id);
CREATE INDEX search_docs_brand_ord ON search_docs (brand_slug, ord, family_id);
CREATE INDEX search_docs_price ON search_docs (price_cents);
-- +goose Down
DROP INDEX IF EXISTS search_docs_ord, search_docs_room_ord, search_docs_cat_ord, search_docs_brand_ord, search_docs_price;
ALTER TABLE search_docs DROP COLUMN ord, DROP COLUMN ord_cat;
