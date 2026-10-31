-- +goose Up
-- list price: the maker's published price (msrp when we have it; a price-book price is a list price)
CREATE OR REPLACE FUNCTION list_cents(src text, price bigint, msrp bigint) RETURNS bigint LANGUAGE sql IMMUTABLE AS $$
  SELECT COALESCE(msrp, CASE WHEN src='catalog' THEN price END)
$$;
-- George's price: site price when we scraped one, else list × brand multiplier
CREATE OR REPLACE FUNCTION our_cents(src text, price bigint, msrp bigint, mult numeric) RETURNS bigint LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN src='site' THEN price ELSE round(price * COALESCE(mult, 0.75))::bigint END
$$;
-- +goose Down
DROP FUNCTION IF EXISTS list_cents(text,bigint,bigint);
DROP FUNCTION IF EXISTS our_cents(text,bigint,bigint,numeric);
