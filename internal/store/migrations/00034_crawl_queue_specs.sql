-- +goose Up
-- Resumable site crawl queue (sitemap → one product page per family) and spec-sheet fetch state.
CREATE TABLE IF NOT EXISTS crawl_queue (
  url         text PRIMARY KEY,
  kind        text NOT NULL,                      -- product | brand | category | other
  brand_slug  text,
  priority    smallint NOT NULL DEFAULT 0,
  status      text NOT NULL DEFAULT 'pending',    -- pending | done | covered | error
  attempts    smallint NOT NULL DEFAULT 0,
  last_error  text,
  covered_by  text,                               -- the family page whose finish dropdown listed this URL
  enqueued_at timestamptz NOT NULL DEFAULT now(),
  fetched_at  timestamptz
);
CREATE INDEX IF NOT EXISTS crawl_queue_pending ON crawl_queue (brand_slug, url) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS crawl_queue_status  ON crawl_queue (status, kind);

-- Seed from what has already been crawled (crawl_state) and the sibling finishes those pages
-- listed (products.url), so the first `crawl run` does not refetch 5.5k pages.
INSERT INTO crawl_queue (url, kind, status, fetched_at, brand_slug)
SELECT cs.url, 'product', 'done', cs.last_fetch,
  (SELECT b.slug FROM brands b WHERE cs.url ILIKE '%/Pasadena-California/' || b.slug || '-%' ORDER BY length(b.slug) DESC LIMIT 1)
FROM crawl_state cs WHERE cs.kind = 'product' AND cs.status = 200 AND cs.url LIKE 'https://www.georgesshowroom.com/Pasadena-California/%'
ON CONFLICT (url) DO NOTHING;

INSERT INTO crawl_queue (url, kind, status, brand_slug)
SELECT p.url, 'product', 'covered', b.slug
FROM products p JOIN brands b ON b.id = p.brand_id
WHERE p.source = 'site' AND p.url LIKE 'https://www.georgesshowroom.com/Pasadena-California/%'
ON CONFLICT (url) DO NOTHING;

ALTER TABLE documents ADD COLUMN IF NOT EXISTS bytes     int;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS pages     int;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS images_at timestamptz;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS error     text;
CREATE INDEX IF NOT EXISTS documents_unfetched ON documents (id) WHERE path IS NULL;

-- +goose Down
DROP INDEX IF EXISTS documents_unfetched;
ALTER TABLE documents DROP COLUMN IF EXISTS bytes, DROP COLUMN IF EXISTS pages, DROP COLUMN IF EXISTS images_at, DROP COLUMN IF EXISTS error;
DROP TABLE IF EXISTS crawl_queue;
