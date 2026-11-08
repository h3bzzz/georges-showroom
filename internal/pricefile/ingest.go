package pricefile

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Stats summarise one ingest.
type Stats struct {
	Rows, Matched, Updated, UnknownBrand, UnknownSKU int
	Brands                                           map[string]int // feed brand → matched rows
	Missing                                          map[string]int // feed brand → rows with no item
}

// Ingester writes feed rows into items.
type Ingester struct {
	Pool   *pgxpool.Pool
	DryRun bool
	// OnlyBrand limits the run to one Unilog brand code.
	OnlyBrand string
}

var reSKUNorm = regexp.MustCompile(`[^A-Z0-9]`)

func normSKU(s string) string { return reSKUNorm.ReplaceAllString(strings.ToUpper(s), "") }

// Run loads every source. A feed row updates the item with the same brand and normalised
// SKU: list price becomes the published price (price_cents), cost goes to cost_cents, UPC
// fills in when missing, and price_source marks the row so catalog OCR never overrides it.
func (in *Ingester) Run(ctx context.Context, sources []*Source) (*Stats, error) {
	st := &Stats{Brands: map[string]int{}, Missing: map[string]int{}}
	brandID, err := in.brandMap(ctx)
	if err != nil {
		return nil, err
	}
	// every item key in memory: 77k rows is small, and it turns the join into a map lookup
	type key struct {
		brand int
		sku   string
	}
	items := map[key]int64{}
	rows, err := in.Pool.Query(ctx, `SELECT id, brand_id, sku_norm FROM items`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var b int
		var n string
		if err := rows.Scan(&id, &b, &n); err != nil {
			rows.Close()
			return nil, err
		}
		items[key{b, n}] = id
	}
	rows.Close()
	var only int
	if in.OnlyBrand != "" {
		only = brandID[norm(in.OnlyBrand)]
		if only == 0 {
			return nil, fmt.Errorf("unknown brand %q", in.OnlyBrand)
		}
	}
	batch := &pgx.Batch{}
	flush := func() error {
		if batch.Len() == 0 || in.DryRun {
			batch = &pgx.Batch{}
			return nil
		}
		br := in.Pool.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return err
			}
		}
		batch = &pgx.Batch{}
		return br.Close()
	}
	t := time.Now()
	for _, src := range sources {
		if !src.Cols.Complete() {
			slog.Warn("pricefile: sheet skipped, no SKU/price columns found", "sheet", src.Name, "headers", src.Headers)
			continue
		}
		slog.Info("pricefile: reading", "sheet", src.Name, "sku", src.Cols.SKU, "brand", src.Cols.Brand, "list", src.Cols.List, "cost", src.Cols.Cost, "price", src.Cols.Price, "upc", src.Cols.UPC)
		var rerr error
		err := src.Rows(func(r Row) bool {
			st.Rows++
			bid := brandID[norm(r.Brand)]
			if bid == 0 {
				st.UnknownBrand++
				st.Missing[r.Brand]++
				return true
			}
			if only != 0 && bid != only {
				return true
			}
			id, ok := items[key{bid, normSKU(r.SKU)}]
			if !ok {
				st.UnknownSKU++
				st.Missing[r.Brand]++
				return true
			}
			st.Matched++
			st.Brands[r.Brand]++
			published := r.PriceCents
			if published == 0 {
				published = r.ListCents
			}
			if published == 0 && r.CostCents == 0 {
				return true
			}
			st.Updated++
			batch.Queue(`
				UPDATE items SET
				  price_cents = CASE WHEN $2 > 0 THEN $2 ELSE price_cents END,
				  list_cents  = CASE WHEN $3 > 0 THEN $3 ELSE list_cents END,
				  cost_cents  = CASE WHEN $4 > 0 THEN $4 ELSE cost_cents END,
				  upc         = COALESCE(NULLIF($5,''), upc),
				  lead_time_days = CASE WHEN $6 > 0 THEN $6 ELSE lead_time_days END,
				  discontinued = $7,
				  price_flag  = CASE WHEN $2 > 0 THEN NULL ELSE price_flag END,
				  price_source = 'pricefile', price_updated_at = now()
				WHERE id = $1`, id, published, r.ListCents, r.CostCents, r.UPC, r.LeadDays, r.Discontinued)
			if batch.Len() >= 2000 {
				if err := flush(); err != nil {
					rerr = err
					return false
				}
			}
			if st.Rows%100000 == 0 {
				slog.Info("pricefile: progress", "rows", st.Rows, "matched", st.Matched, "took", time.Since(t).Round(time.Second))
			}
			return true
		})
		if err != nil {
			return st, err
		}
		if rerr != nil {
			return st, rerr
		}
	}
	if err := flush(); err != nil {
		return st, err
	}
	if !in.DryRun && st.Updated > 0 {
		if _, err := in.Pool.Exec(ctx, `
			UPDATE families f SET price_min_cents = p.lo, price_max_cents = p.hi
			FROM (SELECT family_id, min(price_cents) lo, max(price_cents) hi FROM items WHERE price_cents > 0 AND price_flag IS NULL AND NOT discontinued GROUP BY family_id) p
			WHERE p.family_id = f.id AND (f.price_min_cents IS DISTINCT FROM p.lo OR f.price_max_cents IS DISTINCT FROM p.hi)`); err != nil {
			return st, err
		}
	}
	return st, nil
}

// brandMap keys every brand by normalised code, slug and name.
func (in *Ingester) brandMap(ctx context.Context) (map[string]int, error) {
	rows, err := in.Pool.Query(ctx, `SELECT id, COALESCE(code,''), slug, name FROM brands`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int{}
	for rows.Next() {
		var id int
		var code, slug, name string
		if err := rows.Scan(&id, &code, &slug, &name); err != nil {
			return nil, err
		}
		for _, k := range []string{code, slug, name} {
			if n := norm(k); n != "" {
				m[n] = id
			}
		}
	}
	return m, rows.Err()
}

var reNorm = regexp.MustCompile(`[^a-z0-9]`)

func norm(s string) string { return reNorm.ReplaceAllString(strings.ToLower(s), "") }
