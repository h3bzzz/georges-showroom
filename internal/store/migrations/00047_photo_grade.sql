-- +goose Up
-- family_best.photo_grade: how trustworthy the card picture is, computed by RefreshFamilyBest.
--   0 a vendor master, or a crop the vision model is sure is a whole-product photo whose
--     subject suits the family, with a sane aspect and size
--   1 an acceptable picture (series opener, spec-sheet photo, lower-confidence photo)
--   2 dubious: a detail shot, a callout diagram, an extreme crop, a tiny crop, a subject that
--     contradicts the category, a room scene
--   3 no picture
-- Browse orders dubious and missing pictures behind the rest so the first pages of any query
-- show whole products.
ALTER TABLE family_best ADD COLUMN IF NOT EXISTS photo_grade smallint;

-- +goose StatementBegin
-- "callout": a product photo overprinted with lettered or numbered markers (a Chicago Faucets
-- shower system with a, b, c in circles) is an illustration, never the card photo.
CREATE OR REPLACE FUNCTION crop_kind(pkind text, vkind text, vscore real) RETURNS text
LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE
    WHEN vkind IS NULL OR vscore IS NULL THEN pkind
    WHEN pkind IN ('table','text','blank') THEN pkind
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('drawing','spec') THEN 'drawing'
    WHEN pkind = 'photo' AND vscore >= 0.5 AND vkind = 'callout' THEN 'drawing'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind = 'detail' THEN 'detail'
    WHEN pkind = 'photo' AND vscore >= 0.6 AND vkind IN ('text','other') THEN 'text'
    WHEN pkind = 'drawing' AND vscore >= 0.6 AND vkind = 'photo' THEN 'photo'
    ELSE pkind
  END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- junk titles: a running page heading "(continued)", a warranty or care line, a promotion
-- ("Include ZeroDrain with any …") are not products
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
                OR title ~* '\m(only|holes|interior|depth|overview|stock|special order|continued|warranty|guarantee|care guide|care kit|cleaning|see page|see pages|price list|pricelist|index)\M'
                OR title ~* '\minclude .* with any\M') AS junk) x;
$$;
-- +goose StatementEnd
-- a stored generated column is recomputed only when its row is written
UPDATE product_families SET title = title
 WHERE title ~* '\m(continued|warranty|guarantee|care guide|care kit|cleaning|see page|see pages|price list|pricelist|index)\M' OR title ~* '\minclude .* with any\M';
-- +goose Down
ALTER TABLE family_best DROP COLUMN IF EXISTS photo_grade;
