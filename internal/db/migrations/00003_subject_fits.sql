-- +goose Up
-- +goose StatementBegin
-- subject_fits mirrors taxonomy.SubjectFits for the two cross-family fallbacks that run in SQL.
CREATE OR REPLACE FUNCTION subject_fits(subject text, cat text) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN subject IS NULL OR subject = '' THEN true
    WHEN cat IS NULL OR cat = '' THEN subject <> 'other'
    WHEN subject = 'faucet'    THEN cat = ANY('{Bathroom Sink Faucets,Tub Fillers,Bidet Faucets,Kitchen Faucets,Bar & Prep Faucets,Pot Filler Faucets,Roman Tub Faucets With Hand Showers,Water Filtration,Shower Systems,Faucet Parts,Single Hole,Widespread,Wall Mounted,Deck Mount,Bar Mount}'::text[])
    WHEN subject = 'handle'    THEN cat = ANY('{Faucet Parts,Bathtub Parts,Levers,Knobs,Handles,Cabinet Pulls,Handle Sets,Door Hardware}'::text[])
    WHEN subject = 'trim'      THEN cat = ANY('{Shower Faucet Trims,Thermostatic Valve Trim,Pressure Balance Valve Trims,Pressure Balance Trims With Integrated Diverter,Volume Controls,Shower Systems,Faucet Parts,Rough-In Valves,Bathtub Parts}'::text[])
    WHEN subject = 'valve'     THEN cat = ANY('{Rough-In Valves,Volume Controls,Faucet Parts,Bathtub Parts,Shower Systems,Water Filtration}'::text[])
    WHEN subject = 'shower'    THEN cat = ANY('{Shower Heads,Hand Showers,Shower Systems,Shower Arms,Body Sprays,Hand Shower Slide Bars,Shower Faucet Trims,Tub Fillers,Roman Tub Faucets With Hand Showers}'::text[])
    WHEN subject = 'sink'      THEN cat = ANY('{Bathroom Sinks,Kitchen Sinks,Vanities}'::text[])
    WHEN subject = 'tub'       THEN cat = 'Bathtubs'
    WHEN subject = 'toilet'    THEN cat = ANY('{Toilets,Bidets,Urinals}'::text[])
    WHEN subject = 'part'      THEN cat = ANY('{Faucet Parts,Rough-In Valves,Bathtub Parts,Drains,Garbage Disposals,Water Filtration,Shower Arms,Kitchen Accessories,Lighting}'::text[])
    WHEN subject = 'accessory' THEN cat = ANY('{Towel Bars & Rings,Paper Holders,Grab Bars,Bath Accessories,Towel Warmers,Kitchen Accessories,Water Filtration,Mirrors,Lighting}'::text[])
    WHEN subject = 'cabinet'   THEN cat = ANY('{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware}'::text[])
    WHEN subject = 'hardware'  THEN cat = ANY('{Knobs,Knob,Cabinet Pulls,Handles,Hinges,Bath Accessories,Door Hardware,Levers,Handle Sets,Deadbolts & Locks,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[])
    WHEN subject = 'doorware'  THEN cat = ANY('{Levers,Knobs,Knob,Handles,Handle Sets,Deadbolts & Locks,Hinges,Door Hardware,Dummy,Sets,Passage,Privacy,Keyed,Single Cylinder,Door Stops & Bumpers}'::text[])
    WHEN subject = 'appliance' THEN cat = ANY('{Ranges,Cooktops,Wall Ovens,Refrigerators,Dishwashers,Range Hoods,Coffee Systems,Laundry,Microwaves,Wine Storage}'::text[])
    WHEN subject = 'vanity'    THEN cat = ANY('{Vanities,Medicine Cabinets,Mirrors,Bathroom Sinks}'::text[])
    WHEN subject = 'mirror'    THEN cat = ANY('{Mirrors,Medicine Cabinets,Vanities}'::text[])
    WHEN subject = 'lighting'  THEN cat = ANY('{Lighting,Mirrors,Medicine Cabinets,Bath Accessories}'::text[])
    WHEN subject = 'electrical' THEN cat = ANY('{Door Hardware,Lighting,Bath Accessories}'::text[])
    ELSE false
  END
$$;
-- +goose StatementEnd
-- +goose Down
DROP FUNCTION IF EXISTS subject_fits(text, text);
