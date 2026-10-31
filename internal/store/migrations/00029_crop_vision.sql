-- +goose Up
-- CLIP classification of catalog crops. pkind keeps the pixel/OCR verdict (catalogs audit);
-- vkind/vsubject come from the vision model (catalogs vision); kind is the effective value
-- every query reads, recomputed by crop_kind() whenever either side changes.
ALTER TABLE catalog_crops
  ADD COLUMN IF NOT EXISTS pkind text,
  ADD COLUMN IF NOT EXISTS vkind text,
  ADD COLUMN IF NOT EXISTS vscore real,
  ADD COLUMN IF NOT EXISTS vsubject text,
  ADD COLUMN IF NOT EXISTS vsubscore real,
  ADD COLUMN IF NOT EXISTS vemb bytea,
  ADD COLUMN IF NOT EXISTS vmodel text;
UPDATE catalog_crops SET pkind = kind WHERE pkind IS NULL;
CREATE INDEX IF NOT EXISTS catalog_crops_vmodel ON catalog_crops (vmodel);

-- +goose StatementBegin
-- Effective crop kind. Print evidence (OCR words, ruled rows) is trusted as is; the vision
-- model may demote a pixel "photo" to drawing (incl. dimensioned renders), detail (close-up)
-- or text when it is confident, and rescue a pixel "drawing" that it is sure is a photo
-- (thresholds revised in 00030).
CREATE OR REPLACE FUNCTION crop_kind(pkind text, vkind text, vscore real) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN vkind IS NULL OR vscore IS NULL THEN pkind
    WHEN pkind IN ('table','text','blank') THEN pkind
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('drawing','spec') THEN 'drawing'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind = 'detail' THEN 'detail'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('text','other') THEN 'text'
    WHEN pkind = 'drawing' AND vscore >= 0.9 AND vkind = 'photo' THEN 'photo'
    ELSE pkind
  END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- May a crop showing this subject illustrate a family in these categories? Uncategorised
-- families accept anything but non-products.
CREATE OR REPLACE FUNCTION subject_fits(subject text, cats text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN subject IS NULL THEN true
    WHEN cats IS NULL OR cardinality(cats) = 0 THEN subject <> 'other'
    WHEN subject = 'faucet'    THEN cats && '{Bathroom Sink Faucets,Tub Fillers,Bidet Faucets,Kitchen Faucets,Bar & Prep Faucets,Pot Filler Faucets,Roman Tub Faucets With Hand Showers,Water Filtration,Shower Systems,Faucet Parts,Single Hole,Widespread,Wall Mounted,Deck Mount,Bar Mount}'::text[]
    WHEN subject = 'handle'    THEN cats && '{Faucet Parts,Bathtub Parts,Levers,Knobs,Handles,Cabinet Pulls,Handle Sets,Door Hardware}'::text[]
    WHEN subject = 'trim'      THEN cats && '{Shower Faucet Trims,Thermostatic Valve Trim,Pressure Balance Valve Trims,Pressure Balance Trims With Integrated Diverter,Volume Controls,Shower Systems,Faucet Parts,Rough-In Valves,Bathtub Parts}'::text[]
    WHEN subject = 'valve'     THEN cats && '{Rough-In Valves,Volume Controls,Faucet Parts,Bathtub Parts,Shower Systems,Water Filtration}'::text[]
    WHEN subject = 'shower'    THEN cats && '{Shower Heads,Hand Showers,Shower Systems,Shower Arms,Body Sprays,Hand Shower Slide Bars,Shower Faucet Trims,Tub Fillers,Roman Tub Faucets With Hand Showers}'::text[]
    WHEN subject = 'sink'      THEN cats && '{Bathroom Sinks,Kitchen Sinks,Vanities}'::text[]
    WHEN subject = 'tub'       THEN cats && '{Bathtubs}'::text[]
    WHEN subject = 'toilet'    THEN cats && '{Toilets,Bidets,Urinals}'::text[]
    WHEN subject = 'part'      THEN cats && '{Faucet Parts,Rough-In Valves,Bathtub Parts,Drains,Garbage Disposals,Water Filtration,Shower Arms,Kitchen Accessories,Lighting}'::text[]
    WHEN subject = 'accessory' THEN cats && '{Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Towel Warmers,Kitchen Accessories,Water Filtration,Mirrors,Lighting}'::text[]
    WHEN subject = 'cabinet'   THEN cats && '{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware}'::text[]
    WHEN subject = 'doorware'  THEN cats && '{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[]
    WHEN subject = 'appliance' THEN cats && '{Ranges,Cooktops,Wall Ovens,Refrigerators,Dishwashers,Range Hoods,Coffee Systems,Laundry,Microwaves,Wine Storage}'::text[]
    ELSE false
  END
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS subject_fits(text, text[]);
DROP FUNCTION IF EXISTS crop_kind(text, text, real);
DROP INDEX IF EXISTS catalog_crops_vmodel;
ALTER TABLE catalog_crops
  DROP COLUMN IF EXISTS vmodel, DROP COLUMN IF EXISTS vemb, DROP COLUMN IF EXISTS vsubscore,
  DROP COLUMN IF EXISTS vsubject, DROP COLUMN IF EXISTS vscore, DROP COLUMN IF EXISTS vkind,
  DROP COLUMN IF EXISTS pkind;
