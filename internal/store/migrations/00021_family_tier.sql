-- +goose Up
-- Merchandising tier per family: 0 = premier fixtures (faucets, sinks, tubs, ranges, knobs…),
-- 1 = everything else, 2 = parts (roughs, cartridges, stems, kits…). "Featured" sorts by it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION family_tier(room text, title text, ptype text, cats text[]) RETURNS smallint
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN s ~* '(rough|cartridge|\mparts?\M|aerator|\mstems?\M|escutcheon|flange|adapter|extension|\mhoses?\M|bracket|mounting|repair|spare|\mbody\M|supply|\mstops?\M|\mkit\M|spindle|strike|latch|template|replacement|valve only|in-wall|rough-in|diverter valve|cartridge)' THEN 2
    WHEN room = 'hardware'   AND s ~* '(knob|pull|lever|handleset|entry|hinge|deadbolt|cabinet|door)' THEN 0
    WHEN room = 'appliances' AND s ~* '(range|refrigerat|dishwasher|oven|cooktop|hood|wine|freezer|ice maker|coffee|washer|dryer)' THEN 0
    WHEN room IN ('bathroom','kitchen') AND s ~* '(faucet|\msinks?\M|basin|lavatory|bathtub|\mtubs?\M|freestanding|shower system|showerhead|shower head|rain head|rainhead|toilet|vanit|mirror|pot filler|bidet|tub filler|column)' THEN 0
    ELSE 1 END
  FROM (SELECT concat_ws(' ', title, ptype, array_to_string(cats, ' ')) AS s) x;
$$;
-- +goose StatementEnd
ALTER TABLE product_families ADD COLUMN IF NOT EXISTS tier smallint
  GENERATED ALWAYS AS (family_tier(room, title, product_type, category_path)) STORED;
CREATE INDEX IF NOT EXISTS product_families_room_tier ON product_families (room, tier);

-- +goose Down
DROP INDEX IF EXISTS product_families_room_tier;
ALTER TABLE product_families DROP COLUMN IF EXISTS tier;
DROP FUNCTION IF EXISTS family_tier(text, text, text, text[]);
