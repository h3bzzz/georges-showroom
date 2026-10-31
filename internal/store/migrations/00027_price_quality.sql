-- +goose Up
-- A quarantined price: the reason and the hidden value ("38× brand median:18619300"). While set,
-- price_cents stays NULL and repricing from price books leaves the row alone.
ALTER TABLE products ADD COLUMN IF NOT EXISTS price_flag text;

-- price_token_regex renders a cents value as the regex of its printed forms, so a stored
-- price-book line can be checked for the token it supposedly came from:
-- 178700 → '1,?787(\.00)?' ; 32750 → '327\.50'
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION price_token_regex(cents bigint) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT regexp_replace(
           reverse(regexp_replace(reverse((cents/100)::text), '(\d{3})(?=\d)', '\1,?', 'g')),
           '$', CASE WHEN cents % 100 = 0 THEN '(\.00)?' ELSE '\.' || lpad((cents % 100)::text, 2, '0') END)
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS price_token_regex(bigint);
ALTER TABLE products DROP COLUMN IF EXISTS price_flag;
