-- +goose Up
-- The Unilog data feed (ftp data/<BRAND>-<date>.xml + <BRAND>-EXT-<date>.xml): one row per SKU
-- the company's site carries, with Unilog's own taxonomy, copy, images and attributes. It is
-- the raw store the v2 families/items are derived from for brands the catalog pipeline never
-- reached, and the authority on what is current.
CREATE TABLE feed_products (
  product_id      bigint PRIMARY KEY,
  brand_code      text NOT NULL,
  brand_name      text,
  sku             text NOT NULL,
  sku_norm        text NOT NULL,
  alt_sku         text,
  model           text,
  upc             text,
  cx1_category_id text,
  type            text,                         -- Plumbing | Hardware | Home Decor | Lighting
  grp             text,                         -- Faucets | Showers | Tubs | Cabinet | …
  category        text,
  sub_category    text,
  m_category      text,
  m_sub_category  text,
  collection      text,
  product_name    text,
  finish          text,
  finish_group    text,
  msrp_cents      bigint,
  map_cents       bigint,
  dn_cents        bigint,
  price_group     text,
  currency        text,
  image_name      text,
  image_path      text,
  image_type      smallint,
  alt_images      text[] NOT NULL DEFAULT '{}',
  spec_link       text,
  install_link    text,
  web_link        text,
  description     text,
  marketing       text,
  bullets         text[] NOT NULL DEFAULT '{}',
  attrs           jsonb NOT NULL DEFAULT '{}',  -- every av*/XV_*/at* value not mapped to a column
  created         date,
  updated         date,
  file            text,
  loaded_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feed_products_brand_sku ON feed_products (brand_code, sku_norm);
CREATE INDEX feed_products_name ON feed_products (brand_code, product_name);
CREATE INDEX feed_products_category ON feed_products (category);
-- +goose Down
DROP TABLE IF EXISTS feed_products;
