-- +goose Up
-- RefreshFamilyBest lends a photo-less family the picture of a same-brand family with the
-- same title (one product across series: California Faucets' "Complete Handshower & Diverter
-- for Roman Tub" prints once with a photo and six more times as bare SKUs).
CREATE INDEX IF NOT EXISTS product_families_brand_title ON product_families (brand_id, title) WHERE title IS NOT NULL AND title <> '';
-- +goose Down
DROP INDEX IF EXISTS product_families_brand_title;
