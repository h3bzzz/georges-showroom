-- +goose Up
-- Typed product facts mined from vendor catalogs (internal/catinfo): dimensions, flow rate,
-- mounting, holes, material, certifications, collection, bullets, available finishes…
-- One row per (family, key, source); the merge into product_families keeps site-provided
-- attributes and only fills what the site left empty.
CREATE TABLE IF NOT EXISTS family_facts (
  family_id  bigint NOT NULL REFERENCES product_families(id) ON DELETE CASCADE,
  key        text NOT NULL,
  value      text NOT NULL,
  unit       text,
  source     text NOT NULL DEFAULT 'catalog',
  catalog_id int REFERENCES catalogs(id) ON DELETE SET NULL,
  page       int,
  conf       real,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (family_id, key, source)
);
CREATE INDEX IF NOT EXISTS family_facts_key ON family_facts (key);

-- +goose Down
DROP TABLE IF EXISTS family_facts;
