-- +goose Up
-- family_tier demoted anything whose text had the word "body" (meant for "valve body"), which
-- sent Water Inc's Body Glove filter systems and every body spray to the parts tier.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION family_tier(room text, title text, ptype text, cats text[]) RETURNS smallint
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN s ~* '(rough|cartridge|\mparts?\M|aerator|\mstems?\M|escutcheon|flange|adapter|extension|\mhoses?\M|bracket|mounting|repair|spare|valve body|\mbody only|rough body|supply|\mstops?\M|\mkit\M|spindle|strike|latch|template|replacement|valve only|in-wall|rough-in|diverter valve)' THEN 2
    WHEN junk THEN 1
    WHEN room = 'hardware'   AND s ~* '(knob|pull|lever|handleset|entry|hinge|deadbolt|cabinet|door)' THEN 0
    WHEN room = 'appliances' AND s ~* '(range|refrigerat|dishwasher|oven|cooktop|hood|wine|freezer|ice maker|coffee|washer|dryer)' THEN 0
    WHEN room IN ('bathroom','kitchen') AND t ~* '(faucet|\msinks?\M|basin|lavatory|bathtub|\mtubs?\M|freestanding|shower system|showerhead|shower head|rain head|rainhead|toilet|vanit|mirror|pot filler|bidet|tub filler|column|filter system|filtration system|medicine cabinet)'
         AND t !~* '\m(grid|strainer|holder|basket|hook|grip|spout|overview|stock|special order|food service|drain|stopper|cap|plug|handle|handles|lever|knob|trim|hole|holes|only|accessor|clips?|screws?|compatible|coordinating|waste|overflow|listed|cover|fitting|fittings)' THEN 0
    ELSE 1 END
  FROM (SELECT coalesce(title,'') AS t,
               concat_ws(' ', title, ptype, array_to_string(cats, ' ')) AS s,
               (coalesce(title,'') = '' OR title ~ '[:_"(]' OR title ~ '\d{4,}' OR title ~ '\d[A-Za-z]' OR length(title) > 70
                OR title ~ '\.$' OR title ~* '^(all|have|with|no|the|and|or|for|only|type)\M'
                OR title ~* '\m(only|holes|interior|depth|overview|stock|special order)\M') AS junk) x;
$$;
-- +goose StatementEnd
-- a stored generated column is recomputed only when its row is written
UPDATE product_families SET title = title;
-- +goose Down
SELECT 1;
