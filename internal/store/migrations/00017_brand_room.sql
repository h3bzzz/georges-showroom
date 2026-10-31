-- +goose Up
-- brand_room: the room most of a brand's families live in (Brizo → bathroom, Emtek → hardware),
-- so room imagery is only ever drawn from that room's brands.
CREATE OR REPLACE FUNCTION brand_room(bid bigint) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT room FROM product_families WHERE brand_id=bid AND room IS NOT NULL
  GROUP BY room ORDER BY count(*) DESC LIMIT 1
$$;
-- +goose Down
DROP FUNCTION IF EXISTS brand_room(bigint);
