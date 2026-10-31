-- +goose Up
-- 178700 → '1,?787(\.00)?' ; 32750 → '327\.50' ; 123456700 → '1,?234,?567(\.00)?'
-- (the first version built the grouping by reversing the string, which also reversed ",?")
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION price_token_regex(cents bigint) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT replace(to_char(cents/100, 'FM999,999,999'), ',', ',?')
         || CASE WHEN cents % 100 = 0 THEN '(\.00)?' ELSE '\.' || lpad((cents % 100)::text, 2, '0') END
$$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
