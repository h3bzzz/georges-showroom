package store

import (
	"context"
	"fmt"

	"georges_/internal/pricing"
)

// RevalidateItemPrices re-reads every stored price-book line with the strict money parser and
// clears prices that no longer qualify: index-page references ("186,193"), bare page numbers,
// SKUs read as money, and anything outside pricing's hard bounds. Returns rows cleared.
//
// It works from the stored line text so the whole corpus is fixed without re-running OCR; the
// pipeline's next pass stores strict prices to begin with.
func (s *Store) RevalidateItemPrices(ctx context.Context, brandCode string) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE catalog_items i SET price_cents = NULL
		FROM catalogs c
		WHERE c.id=i.catalog_id AND ($1='' OR c.brand_code=$1) AND i.price_cents IS NOT NULL AND NOT (
		  i.price_cents BETWEEN $2 AND $3
		  AND i.line ~ ('(^|[[:space:]])\$?[[:space:]]?(' || price_token_regex(i.price_cents) || ')([^0-9]|$)')
		)`, brandCode, pricing.MinCents, pricing.MaxCents)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PriceOutlier is a product whose catalog price falls outside its brand's fence.
type PriceOutlier struct {
	ProductID  int64
	Brand      string
	SKU        string
	Title      string
	PriceCents int64
	Lo, Hi     int64
	Median     int64
	Reason     string
}

// AuditPrices fences every brand's catalog list prices (pricing.NewFence on the brand's own
// population) and returns the products that fall outside. Site prices are never audited: they
// are what George's actually charges.
func (s *Store) AuditPrices(ctx context.Context, brandCode string) ([]PriceOutlier, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, b.name, p.sku, COALESCE(f.title,''), p.price_cents, b.id, COALESCE(f.tier,1)
		FROM products p JOIN brands b ON b.id=p.brand_id LEFT JOIN product_families f ON f.id=p.family_id
		WHERE p.source='catalog' AND p.price_cents > 0 AND ($1='' OR b.code=$1)
		ORDER BY b.id, p.price_cents`, brandCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		PriceOutlier
		brandID int64
		tier    int
	}
	var all []row
	byBrand := map[int64][]int64{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ProductID, &r.Brand, &r.SKU, &r.Title, &r.PriceCents, &r.brandID, &r.tier); err != nil {
			return nil, err
		}
		all = append(all, r)
		byBrand[r.brandID] = append(byBrand[r.brandID], r.PriceCents)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fences := make(map[int64]pricing.Fence, len(byBrand))
	for id, v := range byBrand {
		fences[id] = pricing.NewFence(v)
	}
	var out []PriceOutlier
	for _, r := range all {
		f := fences[r.brandID]
		if f.Plausible(r.PriceCents) && r.PriceCents <= pricing.TierMax(r.tier) {
			continue
		}
		o := r.PriceOutlier
		o.Lo, o.Hi, o.Median = f.Lo, f.Hi, f.Median
		switch {
		case r.PriceCents > pricing.TierMax(r.tier) && f.Plausible(r.PriceCents):
			o.Reason = fmt.Sprintf("above tier-%d maximum %s", r.tier, moneyShort(pricing.TierMax(r.tier)))
		case r.PriceCents > pricing.MaxCents:
			o.Reason = "above hard maximum"
		case r.PriceCents < pricing.MinCents:
			o.Reason = "below hard minimum"
		case r.PriceCents > f.Hi:
			o.Reason = fmt.Sprintf("%.0f× brand median", float64(r.PriceCents)/float64(f.Median))
		default:
			o.Reason = fmt.Sprintf("1/%.0f of brand median", float64(f.Median)/float64(r.PriceCents))
		}
		out = append(out, o)
	}
	return out, nil
}

// QuarantinePrices hides the given outliers: the price moves to price_flag (so it can be
// inspected and so a re-run does not silently restore it) and price_cents becomes NULL, which
// the storefront renders as "Call for price". Returns rows changed.
func (s *Store) QuarantinePrices(ctx context.Context, outliers []PriceOutlier) (int64, error) {
	var n int64
	for _, o := range outliers {
		tag, err := s.Pool.Exec(ctx, `
			UPDATE products SET price_flag = $2 || ':' || price_cents::text, price_cents = NULL
			WHERE id=$1 AND price_cents IS NOT NULL`, o.ProductID, o.Reason)
		if err != nil {
			return n, err
		}
		n += tag.RowsAffected()
	}
	return n, nil
}

// PriceStats summarises the catalog price population for a brand (or all) after a repricing.
type PriceStats struct {
	Priced, Flagged, Unpriced int64
	Median, P99, Max          int64
}

func (s *Store) PriceStats(ctx context.Context, brandCode string) (PriceStats, error) {
	var st PriceStats
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE p.price_cents > 0),
		       count(*) FILTER (WHERE p.price_flag IS NOT NULL),
		       count(*) FILTER (WHERE COALESCE(p.price_cents,0)=0),
		       COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY p.price_cents) FILTER (WHERE p.price_cents>0),0)::bigint,
		       COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY p.price_cents) FILTER (WHERE p.price_cents>0),0)::bigint,
		       COALESCE(max(p.price_cents),0)
		FROM products p JOIN brands b ON b.id=p.brand_id
		WHERE p.source='catalog' AND ($1='' OR b.code=$1)`, brandCode).
		Scan(&st.Priced, &st.Flagged, &st.Unpriced, &st.Median, &st.P99, &st.Max)
	return st, err
}

// ResetPriceFlags lifts every quarantine for a brand (or all); the next repricing restores the
// price-book value and the audit judges it again with the current fence.
func (s *Store) ResetPriceFlags(ctx context.Context, brandCode string) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE products p SET price_flag = NULL, price_cents = NULL
		FROM brands b WHERE b.id=p.brand_id AND p.price_flag IS NOT NULL AND ($1='' OR b.code=$1)`, brandCode)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func moneyShort(c int64) string { return fmt.Sprintf("$%d", c/100) }
