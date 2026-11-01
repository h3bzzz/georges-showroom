-- +goose Up
-- Sizes parsed from attributes (internal/store/dims.go) for range retrieval ("66 inch tub"),
-- and a GIN index so spec facets (attributes @> '{"Mounting":"Wall Mount"}') stay quick.
CREATE TABLE IF NOT EXISTS family_dims (
  family_id bigint NOT NULL REFERENCES product_families(id) ON DELETE CASCADE,
  key       text NOT NULL,
  inches    real NOT NULL,
  PRIMARY KEY (family_id, key, inches)
);
CREATE INDEX IF NOT EXISTS family_dims_key_in ON family_dims (key, inches);
CREATE INDEX IF NOT EXISTS family_dims_in ON family_dims (inches);
CREATE INDEX IF NOT EXISTS product_families_attrs ON product_families USING gin (attributes jsonb_path_ops);
-- +goose Down
DROP INDEX IF EXISTS product_families_attrs;
DROP TABLE IF EXISTS family_dims;
