-- +goose Up
-- family_best.photo_how says where the card photo came from: 'crop' (a crop linked to the family
-- by SKU or page geometry) or 'series' (the collection photograph that opens the family's section
-- of the book, used when the family has no picture of its own).
ALTER TABLE family_best ADD COLUMN IF NOT EXISTS photo_how text;
-- medicine cabinets and vanities appear in the vision model's sink/accessory/cabinet scenes
-- +goose StatementBegin
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
    WHEN subject = 'sink'      THEN cats && '{Bathroom Sinks,Kitchen Sinks,Vanities,Medicine Cabinets,Mirrors}'::text[]
    WHEN subject = 'tub'       THEN cats && '{Bathtubs}'::text[]
    WHEN subject = 'toilet'    THEN cats && '{Toilets,Bidets,Urinals}'::text[]
    WHEN subject = 'part'      THEN cats && '{Faucet Parts,Rough-In Valves,Bathtub Parts,Drains,Garbage Disposals,Water Filtration,Shower Arms,Kitchen Accessories,Lighting}'::text[]
    WHEN subject = 'accessory' THEN cats && '{Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Towel Warmers,Kitchen Accessories,Water Filtration,Mirrors,Medicine Cabinets,Lighting}'::text[]
    WHEN subject = 'cabinet'   THEN cats && '{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware,Medicine Cabinets,Vanities}'::text[]
    WHEN subject = 'doorware'  THEN cats && '{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[]
    WHEN subject = 'appliance' THEN cats && '{Ranges,Cooktops,Wall Ovens,Refrigerators,Dishwashers,Range Hoods,Coffee Systems,Laundry,Microwaves,Wine Storage}'::text[]
    ELSE false
  END
$$;
-- +goose StatementEnd
-- +goose Down
ALTER TABLE family_best DROP COLUMN IF EXISTS photo_how;
