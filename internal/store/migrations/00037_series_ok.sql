-- +goose Up
-- series_ok was appended to 00036 after the server had already applied it; goose never reruns
-- a version, so it is (re)created here.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION series_ok(tier smallint, cats text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
  SELECT COALESCE(tier, 1) <> 2
     AND NOT (COALESCE(cats, '{}'::text[]) && '{Faucet Parts,Bathtub Parts,Drains,Garbage Disposals,Rough-In Valves,Volume Controls,Shower Arms,Kitchen Accessories,Water Filtration}'::text[])
$$;
-- +goose StatementEnd
-- +goose Down
DROP FUNCTION IF EXISTS series_ok(smallint, text[]);
