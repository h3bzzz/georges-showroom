-- +goose Up
-- George's v2 schema. One designed migration instead of fifty accreted ones. Business rules live
-- in Go (internal/media, internal/search, internal/taxonomy); the database holds facts and
-- materialized decisions (family_media, search_docs) that the web layer reads with one query.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE brands (
  id               serial PRIMARY KEY,
  code             text UNIQUE,                 -- Unilog brand code (BRZ, CAL, …)
  slug             text UNIQUE NOT NULL,
  name             text NOT NULL,
  room             text,                        -- the room a brand usually sells into
  website          text,
  logo_path        text,                        -- relative to DATA_DIR
  featured         boolean NOT NULL DEFAULT false,
  price_multiplier numeric(5,3) NOT NULL DEFAULT 0.750,
  legacy_id        int
);

-- A family is what a card shows: one product in all its finishes. Items are the SKUs.
CREATE TABLE families (
  id              bigserial PRIMARY KEY,
  brand_id        int NOT NULL REFERENCES brands(id),
  key             text NOT NULL,
  title           text,
  display_title   text,                         -- synthesized when the printed title is junk
  collection      text,
  category        text,                         -- leaf category (taxonomy in Go)
  room            text,                         -- bathroom | kitchen | hardware | appliances
  tier            smallint NOT NULL DEFAULT 1,  -- 0 fixture, 1 ordinary, 2 part/rough-in, 3 junk
  description     text,
  bullets         text[] NOT NULL DEFAULT '{}',
  attributes      jsonb NOT NULL DEFAULT '{}',  -- site attribute table (key → value)
  facts           jsonb NOT NULL DEFAULT '{}',  -- typed facts mined from catalogs (key → {value,unit,source,conf})
  finishes        text[] NOT NULL DEFAULT '{}',
  source          text NOT NULL DEFAULT 'catalog',
  price_min_cents bigint,
  price_max_cents bigint,
  legacy_id       bigint,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (brand_id, key)
);
CREATE INDEX families_brand ON families (brand_id);
CREATE INDEX families_category ON families (room, category);
CREATE INDEX families_legacy ON families (legacy_id);

CREATE TABLE items (
  id             bigserial PRIMARY KEY,
  family_id      bigint NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  brand_id       int NOT NULL REFERENCES brands(id),
  sku            text NOT NULL,
  sku_norm       text NOT NULL,                  -- uppercase, no punctuation: the join key for every source
  finish         text,
  finish_code    text,
  upc            text,
  list_cents     bigint,
  price_cents    bigint,
  price_flag     text,                           -- quarantine reason; NULL when the price is trusted
  lead_time_days int,
  url            text,
  source         text NOT NULL DEFAULT 'catalog',
  legacy_id      bigint,
  UNIQUE (brand_id, sku)
);
CREATE INDEX items_family ON items (family_id);
CREATE INDEX items_sku_norm ON items (sku_norm);
CREATE INDEX items_sku_trgm ON items USING gin (sku gin_trgm_ops);

CREATE TABLE finish_codes (
  brand_code text NOT NULL,
  code       text NOT NULL,
  finish     text NOT NULL,
  PRIMARY KEY (brand_code, code)
);

-- Vendor catalogs (flipbooks) stay browsable as a library and remain the imagery corpus.
CREATE TABLE catalogs (
  id         serial PRIMARY KEY,
  brand_id   int REFERENCES brands(id),
  code       text NOT NULL,
  name       text NOT NULL,
  title      text,
  pages      int,
  has_text   boolean,
  cover_path text,
  legacy_id  int,
  UNIQUE (code, name)
);
CREATE TABLE catalog_pages (
  catalog_id int NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
  page       int NOT NULL,
  path       text,
  text       text,
  search     tsvector GENERATED ALWAYS AS (to_tsvector('simple', left(coalesce(text,''), 200000))) STORED,
  PRIMARY KEY (catalog_id, page)
);
CREATE INDEX catalog_pages_search ON catalog_pages USING gin (search);
CREATE TABLE catalog_toc (
  catalog_id int NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE,
  page       int NOT NULL,
  title      text NOT NULL,
  level      int NOT NULL DEFAULT 1,
  source     text NOT NULL DEFAULT 'ocr',
  PRIMARY KEY (catalog_id, page, title)
);

-- Every picture the site can show is an asset: immutable, content-addressed, measured once.
-- Renditions are derived files named by hash and width (internal/assets), never rows.
CREATE TABLE assets (
  id            bigserial PRIMARY KEY,
  hash          text NOT NULL UNIQUE,           -- sha256 hex of the source bytes
  src_path      text NOT NULL,                  -- relative to DATA_DIR
  format        text,
  width         int,
  height        int,
  bytes         bigint,
  source        text NOT NULL,                  -- vendor | catalog | spec | upload
  origin        jsonb NOT NULL DEFAULT '{}',    -- {catalog_id,page,x,y,w,h} | {url}
  kind          text,                           -- photo | detail | drawing | callout | spec | text | lifestyle | other
  kind_score    real,
  subject       text,
  subject_score real,
  scene         real,                           -- probability the picture is a room scene
  tone          text,
  tone_score    real,
  sharpness     real,                           -- Laplacian-variance proxy, log-scaled 0..1
  bg_white      real,                           -- share of near-white border pixels 0..1
  fill          real,                           -- share of the frame the object occupies 0..1
  quality       real,                           -- composite 0..1 (internal/assets.Quality)
  lqip          int,                            -- Lean LQIP integer (CSS-only placeholder)
  color         text,                           -- dominant border color (#rrggbb)
  phash         bigint,
  pdq           bytea,
  embedding     bytea,
  emb_model     text,
  dup_of        bigint REFERENCES assets(id),
  rejected      boolean NOT NULL DEFAULT false,
  built         smallint NOT NULL DEFAULT 0,    -- rendition ladder version present on disk
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assets_phash ON assets (phash);
CREATE INDEX assets_source ON assets (source);
CREATE INDEX assets_built ON assets (built) WHERE built = 0;

-- How an asset came to be associated with a family (the evidence, not the decision).
CREATE TABLE asset_links (
  asset_id  bigint NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  family_id bigint NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  item_id   bigint REFERENCES items(id) ON DELETE SET NULL,
  how       text NOT NULL,                      -- vendor | spec | sku | family | norm | suffix | near | page | opener | twin
  weight    real NOT NULL DEFAULT 1,
  PRIMARY KEY (asset_id, family_id, how)
);
CREATE INDEX asset_links_family ON asset_links (family_id);

-- The decision: which assets a family shows, in which role and order. Rebuilt by `media pick`.
CREATE TABLE family_media (
  family_id bigint NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  role      text NOT NULL,                      -- card | gallery | drawing | lifestyle
  rank      smallint NOT NULL,
  asset_id  bigint NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  item_id   bigint REFERENCES items(id) ON DELETE SET NULL,
  how       text NOT NULL,
  grade     smallint NOT NULL DEFAULT 1,        -- 0 trusted, 1 acceptable, 2 doubtful
  PRIMARY KEY (family_id, role, rank)
);
CREATE INDEX family_media_asset ON family_media (asset_id);

CREATE TABLE documents (
  id        bigserial PRIMARY KEY,
  family_id bigint REFERENCES families(id) ON DELETE CASCADE,
  item_id   bigint REFERENCES items(id) ON DELETE SET NULL,
  kind      text NOT NULL,                      -- spec | install | care | brochure
  url       text UNIQUE NOT NULL,
  path      text,
  pages     int,
  text      text,
  legacy_id bigint
);
CREATE INDEX documents_family ON documents (family_id);

-- Curated imagery for rooms, category tiles and the home page.
CREATE TABLE covers (
  kind       text NOT NULL,                     -- home | room | category
  key        text NOT NULL,
  path       text NOT NULL,
  catalog_id int,
  page       int,
  caption    text,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, key)
);

-- One row per family that browse, search and typeahead read. Rebuilt by `index build`;
-- no joins at request time.
CREATE TABLE search_docs (
  family_id   bigint PRIMARY KEY REFERENCES families(id) ON DELETE CASCADE,
  brand_id    int NOT NULL,
  brand       text NOT NULL,
  brand_slug  text NOT NULL,
  title       text NOT NULL,
  collection  text,
  room        text,
  category    text,
  groups      text[] NOT NULL DEFAULT '{}',     -- merchandised groups the category belongs to
  tier        smallint NOT NULL,
  grade       smallint NOT NULL DEFAULT 3,      -- card picture grade, 3 = none
  tones       text[] NOT NULL DEFAULT '{}',
  finishes    text[] NOT NULL DEFAULT '{}',
  price_cents bigint,
  card_hash   text,
  card_w      int,
  card_h      int,
  card_lqip   int,
  skus        text[] NOT NULL DEFAULT '{}',
  blob        text NOT NULL,                    -- trigram target: title, collection, skus, finishes
  tsv         tsvector NOT NULL,
  pop         real NOT NULL DEFAULT 0,          -- popularity / merchandising boost
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX search_docs_tsv ON search_docs USING gin (tsv);
CREATE INDEX search_docs_blob ON search_docs USING gin (blob gin_trgm_ops);
CREATE INDEX search_docs_browse ON search_docs (room, category, tier, grade);
CREATE INDEX search_docs_brand ON search_docs (brand_slug);
CREATE INDEX search_docs_groups ON search_docs USING gin (groups);
CREATE INDEX search_docs_skus ON search_docs USING gin (skus);

-- Resumable offline jobs record their cursor here.
CREATE TABLE jobs (
  name        text PRIMARY KEY,
  state       jsonb NOT NULL DEFAULT '{}',
  started_at  timestamptz,
  finished_at timestamptz,
  note        text
);

-- +goose Down
DROP TABLE IF EXISTS jobs, search_docs, covers, documents, family_media, asset_links, assets,
  catalog_toc, catalog_pages, catalogs, finish_codes, items, families, brands;
