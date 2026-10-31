-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE brands (
  id      serial PRIMARY KEY,
  code    text UNIQUE,
  slug    text UNIQUE NOT NULL,
  name    text NOT NULL,
  logo    text
);

CREATE TABLE product_families (
  id            bigserial PRIMARY KEY,
  brand_id      int NOT NULL REFERENCES brands(id),
  family_key    text NOT NULL,
  title         text,
  description   text,
  product_type  text,
  collection    text,
  category_path text[] NOT NULL DEFAULT '{}',
  attributes    jsonb NOT NULL DEFAULT '{}',
  bullets       text[] NOT NULL DEFAULT '{}',
  search        tsvector,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (brand_id, family_key)
);
CREATE INDEX product_families_search ON product_families USING gin (search);
CREATE INDEX product_families_title_trgm ON product_families USING gin (title gin_trgm_ops);

CREATE TABLE products (
  id                bigserial PRIMARY KEY,
  brand_id          int NOT NULL REFERENCES brands(id),
  family_id         bigint REFERENCES product_families(id),
  sku               text NOT NULL,
  upc               text,
  finish            text,
  online_product_id bigint,
  url               text UNIQUE,
  name              text,
  price_cents       bigint,
  msrp_cents        bigint,
  trade_cents       bigint,
  currency          text NOT NULL DEFAULT 'USD',
  quote_id          text,
  lead_time_days    int,
  freight_note      text,
  first_seen        timestamptz NOT NULL DEFAULT now(),
  last_seen         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (brand_id, sku)
);
CREATE INDEX products_sku_trgm ON products USING gin (sku gin_trgm_ops);
CREATE INDEX products_family ON products (family_id);

CREATE TABLE price_history (
  product_id  bigint NOT NULL REFERENCES products(id),
  observed    date NOT NULL,
  price_cents bigint,
  msrp_cents  bigint,
  trade_cents bigint,
  PRIMARY KEY (product_id, observed)
);

CREATE TABLE product_images (
  id         bigserial PRIMARY KEY,
  product_id bigint REFERENCES products(id),
  family_id  bigint REFERENCES product_families(id),
  source     text NOT NULL CHECK (source IN ('site','catalog','spec','upload')),
  url        text,
  path       text,
  position   int NOT NULL DEFAULT 0,
  width      int,
  height     int,
  sha1       text,
  phash      bigint,
  UNIQUE (source, url)
);
CREATE INDEX product_images_product ON product_images (product_id);

CREATE TABLE documents (
  id         bigserial PRIMARY KEY,
  family_id  bigint REFERENCES product_families(id),
  product_id bigint REFERENCES products(id),
  kind       text NOT NULL,
  url        text UNIQUE NOT NULL,
  path       text,
  sha1       text,
  text       text,
  fetched_at timestamptz
);

CREATE TABLE catalogs (
  id          serial PRIMARY KEY,
  brand_code  text NOT NULL,
  name        text NOT NULL,
  title       text,
  pages       int,
  created_raw text,
  has_text    boolean,
  fetched_at  timestamptz,
  UNIQUE (brand_code, name)
);

CREATE TABLE catalog_pages (
  catalog_id int NOT NULL REFERENCES catalogs(id),
  page       int NOT NULL,
  path       text,
  text       text,
  ocr_text   text,
  PRIMARY KEY (catalog_id, page)
);

CREATE TABLE crawl_state (
  url        text PRIMARY KEY,
  kind       text NOT NULL,
  status     int,
  last_fetch timestamptz,
  error      text
);

-- +goose Down
DROP TABLE IF EXISTS crawl_state, catalog_pages, catalogs, documents, product_images,
  price_history, products, product_families, brands;
