-- +goose Up
-- Indexed full-text search over catalog page text (PDF text layer + OCR). The header search
-- previously rebuilt a tsvector over ~14 MB of text per keystroke (~1.5 s).
ALTER TABLE catalog_pages
  ADD COLUMN IF NOT EXISTS search tsvector
  GENERATED ALWAYS AS (to_tsvector('english'::regconfig, coalesce(text,'') || ' ' || coalesce(ocr_text,''))) STORED;
CREATE INDEX IF NOT EXISTS catalog_pages_search ON catalog_pages USING gin (search);
-- substring fallback for SKU-like tokens that the english parser splits apart (e.g. "K-394-4")
CREATE INDEX IF NOT EXISTS catalog_pages_text_trgm ON catalog_pages USING gin (text gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS catalog_pages_text_trgm;
DROP INDEX IF EXISTS catalog_pages_search;
ALTER TABLE catalog_pages DROP COLUMN IF EXISTS search;
