-- +goose Up
-- One row per model number we asked a manufacturer's site for, so misses are not retried
-- on every run and hits can be audited.
CREATE TABLE spec_lookups (
  brand_code text NOT NULL,
  model      text NOT NULL,
  found      boolean NOT NULL,
  url        text,
  tried_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (brand_code, model)
);
-- +goose Down
DROP TABLE IF EXISTS spec_lookups;
