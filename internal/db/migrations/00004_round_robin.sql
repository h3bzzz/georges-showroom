-- +goose Up
-- Precomputed brand round-robin ranks so a room or category page interleaves brands without
-- a window function at request time: rr_room ranks a family within its brand and room,
-- rr_cat within its brand, room and category (both ordered by tier, picture grade, pop).
ALTER TABLE search_docs ADD COLUMN rr_room int NOT NULL DEFAULT 0, ADD COLUMN rr_cat int NOT NULL DEFAULT 0;
CREATE INDEX search_docs_rr_room ON search_docs (room, tier, rr_room);
-- +goose Down
ALTER TABLE search_docs DROP COLUMN rr_room, DROP COLUMN rr_cat;
