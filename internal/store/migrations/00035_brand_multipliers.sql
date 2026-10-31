-- +goose Up
-- George's sells every brand at a fixed fraction of the maker's list price, and the site prints
-- both numbers: price_cents / msrp_cents is constant per brand (Emtek 0.80 on 3,668 SKUs,
-- Watermark and California Faucets 0.75, Baldwin 0.70, Top Knobs 0.90). The placeholder 0.75
-- in brands.price_multiplier is replaced by that observed ratio wherever the site gave one;
-- store.RefreshBrandMultipliers repeats this after every crawl.
UPDATE brands b SET price_multiplier = x.m
FROM (
  SELECT brand_id, round((percentile_cont(0.5) WITHIN GROUP (ORDER BY price_cents::numeric / msrp_cents))::numeric, 2) AS m
  FROM products WHERE source = 'site' AND msrp_cents > 0 AND price_cents > 0
  GROUP BY brand_id
) x
WHERE x.brand_id = b.id AND x.m BETWEEN 0.3 AND 1.0 AND b.price_multiplier IS DISTINCT FROM x.m;
-- +goose Down
UPDATE brands SET price_multiplier = 0.75;
