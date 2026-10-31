-- +goose Up
-- +goose StatementBegin
-- The vision subject head merges cabinet and door hardware into one "hardware" class
-- (vision.SubjectMerge); 00029 only knew the finer names, so every hardware crop failed
-- the category check. Bath hardware (towel bars, hooks) from hardware brands reads as
-- hardware too, so those categories accept it.
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
    WHEN subject = 'sink'      THEN cats && '{Bathroom Sinks,Kitchen Sinks,Vanities}'::text[]
    WHEN subject = 'tub'       THEN cats && '{Bathtubs}'::text[]
    WHEN subject = 'toilet'    THEN cats && '{Toilets,Bidets,Urinals}'::text[]
    WHEN subject = 'part'      THEN cats && '{Faucet Parts,Rough-In Valves,Bathtub Parts,Drains,Garbage Disposals,Water Filtration,Shower Arms,Kitchen Accessories,Lighting,Volume Controls}'::text[]
    WHEN subject = 'accessory' THEN cats && '{Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Towel Warmers,Kitchen Accessories,Water Filtration,Mirrors,Lighting}'::text[]
    WHEN subject = 'cabinet'   THEN cats && '{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware}'::text[]
    WHEN subject = 'doorware'  THEN cats && '{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[]
    WHEN subject = 'hardware'  THEN cats && '{Levers,Knobs,Knob,Handles,Cabinet Pulls,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers,Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Faucet Parts}'::text[]
    WHEN subject = 'appliance' THEN cats && '{Ranges,Cooktops,Wall Ovens,Refrigerators,Dishwashers,Range Hoods,Coffee Systems,Laundry,Microwaves,Wine Storage}'::text[]
    ELSE false
  END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Rescue threshold: every pixel "drawing" the model called a photo at ≥0.6 in a 40-crop sample
-- was a real photo (hardware pull sets, towel warmers, hinges — thin shapes on white).
CREATE OR REPLACE FUNCTION crop_kind(pkind text, vkind text, vscore real) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN vkind IS NULL OR vscore IS NULL THEN pkind
    WHEN pkind IN ('table','text','blank') THEN pkind
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('drawing','spec') THEN 'drawing'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind = 'detail' THEN 'detail'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('text','other') THEN 'text'
    WHEN pkind = 'drawing' AND vscore >= 0.6 AND vkind = 'photo' THEN 'photo'
    ELSE pkind
  END
$$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
