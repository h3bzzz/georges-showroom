-- +goose Up
-- Spec-sheet images (product_images source='spec', extracted from vendor PDFs) get the same
-- CLIP verdict as catalog crops so a true product photograph among them can be a card photo.
ALTER TABLE product_images
  ADD COLUMN IF NOT EXISTS vkind text,
  ADD COLUMN IF NOT EXISTS vscore real,
  ADD COLUMN IF NOT EXISTS vsubject text,
  ADD COLUMN IF NOT EXISTS vsubscore real,
  ADD COLUMN IF NOT EXISTS vemb bytea,
  ADD COLUMN IF NOT EXISTS vmodel text;
CREATE INDEX IF NOT EXISTS product_images_spec_vision ON product_images (family_id) WHERE source = 'spec' AND vkind = 'photo';

-- +goose StatementBegin
-- lighting and electrical subjects (vision.SubjectClasses): Buster + Punch pendants and switch
-- plates read as "other" before and were refused for every family. A lighting photo suits a
-- Lighting family and an uncategorised one; an electrical one only an uncategorised family.
CREATE OR REPLACE FUNCTION subject_fits(subject text, cats text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN subject IS NULL THEN true
    WHEN cats IS NULL OR cardinality(cats) = 0 THEN subject <> 'other'
    WHEN subject = 'faucet'    THEN cats && '{Bathroom Sink Faucets,Tub Fillers,Bidet Faucets,Kitchen Faucets,Bar & Prep Faucets,Pot Filler Faucets,Roman Tub Faucets With Hand Showers,Water Filtration,Shower Systems,Faucet Parts,Single Hole,Widespread,Wall Mounted,Deck Mount,Bar Mount}'::text[]
    WHEN subject = 'handle'    THEN cats && '{Faucet Parts,Bathtub Parts,Levers,Knobs,Knob,Handles,Cabinet Pulls,Handle Sets,Door Hardware,Door Stops & Bumpers}'::text[]
    WHEN subject = 'trim'      THEN cats && '{Shower Faucet Trims,Thermostatic Valve Trim,Pressure Balance Valve Trims,Pressure Balance Trims With Integrated Diverter,Volume Controls,Shower Systems,Faucet Parts,Rough-In Valves,Bathtub Parts}'::text[]
    WHEN subject = 'valve'     THEN cats && '{Rough-In Valves,Volume Controls,Faucet Parts,Bathtub Parts,Shower Systems,Water Filtration}'::text[]
    WHEN subject = 'shower'    THEN cats && '{Shower Heads,Hand Showers,Shower Systems,Shower Arms,Body Sprays,Hand Shower Slide Bars,Shower Faucet Trims,Tub Fillers,Roman Tub Faucets With Hand Showers}'::text[]
    WHEN subject = 'sink'      THEN cats && '{Bathroom Sinks,Kitchen Sinks,Vanities,Medicine Cabinets,Mirrors}'::text[]
    WHEN subject = 'tub'       THEN cats && '{Bathtubs}'::text[]
    WHEN subject = 'toilet'    THEN cats && '{Toilets,Bidets,Urinals}'::text[]
    WHEN subject = 'part'      THEN cats && '{Faucet Parts,Rough-In Valves,Bathtub Parts,Drains,Garbage Disposals,Water Filtration,Shower Arms,Kitchen Accessories,Lighting,Volume Controls}'::text[]
    WHEN subject = 'accessory' THEN cats && '{Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Towel Warmers,Kitchen Accessories,Water Filtration,Mirrors,Medicine Cabinets,Lighting}'::text[]
    WHEN subject = 'cabinet'   THEN cats && '{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware,Medicine Cabinets,Vanities}'::text[]
    WHEN subject = 'doorware'  THEN cats && '{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[]
    WHEN subject = 'hardware'  THEN cats && '{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers,Cabinet Pulls,Bath Accessories,Towel Bars & Rings,Paper Holders,Grab Bars,Shower Faucet Trims,Water Filtration}'::text[]
    WHEN subject = 'appliance' THEN cats && '{Ranges,Cooktops,Wall Ovens,Refrigerators,Dishwashers,Range Hoods,Coffee Systems,Laundry,Microwaves,Wine Storage}'::text[]
    WHEN subject = 'lighting'  THEN cats && '{Lighting,Mirrors,Medicine Cabinets}'::text[]
    WHEN subject = 'electrical' THEN cats && '{Electrical,Switches & Sockets,Lighting}'::text[]
    ELSE false
  END
$$;
-- +goose StatementEnd


-- +goose StatementBegin
-- May a family borrow the collection photograph that opens its section of the book? Parts,
-- rough-ins and drains (tier 2 or a parts category) may not: the opener shows the finished
-- fixture, never the part.
CREATE OR REPLACE FUNCTION series_ok(tier smallint, cats text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
  SELECT COALESCE(tier, 1) <> 2
     AND NOT (COALESCE(cats, '{}'::text[]) && '{Faucet Parts,Bathtub Parts,Drains,Garbage Disposals,Rough-In Valves,Volume Controls,Shower Arms,Kitchen Accessories,Water Filtration}'::text[])
$$;
-- +goose StatementEnd

-- +goose Down
DROP INDEX IF EXISTS product_images_spec_vision;
ALTER TABLE product_images
  DROP COLUMN IF EXISTS vmodel, DROP COLUMN IF EXISTS vemb, DROP COLUMN IF EXISTS vsubscore,
  DROP COLUMN IF EXISTS vsubject, DROP COLUMN IF EXISTS vscore, DROP COLUMN IF EXISTS vkind;
DROP FUNCTION IF EXISTS series_ok(smallint, text[]);
