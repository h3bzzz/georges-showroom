-- +goose Up
-- One row per vendor image URL we asked for, so misses are not retried on every run.
CREATE TABLE vendor_lookups (
  brand_code text NOT NULL,
  url        text NOT NULL,
  found      boolean NOT NULL,
  tried_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (brand_code, url)
);
-- +goose Down
DROP TABLE IF EXISTS vendor_lookups;
