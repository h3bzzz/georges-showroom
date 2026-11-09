package media

import (
	"context"
	"log/slog"
	"math"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Catalog linking for families the v1 pipeline never knew (everything derived from the Unilog
// feed): a photograph cropped from a catalog page belongs to the model numbers printed nearest
// to it on that page. The evidence goes into asset_links as how='near' with a weight that
// falls with distance; a page with one photograph and one or two models links as how='page'.
// The catalog prints a model with or without its finish code, so a printed number also stands
// for every item whose SKU starts with it.

// Box is a rectangle in page pixels.
type Box struct{ X, Y, W, H float64 }

func (b Box) cx() float64 { return b.X + b.W/2 }
func (b Box) cy() float64 { return b.Y + b.H/2 }

// gap is the distance between two rectangles' edges (0 when they touch or overlap), which
// treats a caption directly under a wide photo as close even when their centres are far apart.
func gap(a, b Box) float64 {
	dx := math.Max(0, math.Max(a.X-(b.X+b.W), b.X-(a.X+a.W)))
	dy := math.Max(0, math.Max(a.Y-(b.Y+b.H), b.Y-(a.Y+a.H)))
	return math.Hypot(dx, dy)
}

// Nearest picks, for a model number printed at sku, the photograph it most likely captions:
// the closest by edge gap, within 30 % of the page diagonal. Returns the index into crops and
// a weight in (0.45, 0.9], or -1.
func Nearest(sku Box, crops []Box, diag float64) (int, float32) {
	best, bestD := -1, math.MaxFloat64
	for i, c := range crops {
		d := gap(sku, c)
		// a number above its picture is rarer than one below or beside it: penalise slightly
		if sku.cy() < c.Y {
			d *= 1.25
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 || diag <= 0 || bestD > 0.30*diag {
		return -1, 0
	}
	w := 0.9 - 1.5*(bestD/diag)
	if w < 0.45 {
		w = 0.45
	}
	return best, float32(w)
}

type cropRow struct {
	asset   int64
	catalog int
	page    int
	box     Box
}

type skuRow struct {
	catalog int
	page    int
	norm    string
	box     Box
}

type itemRef struct {
	id, family int64
	norm       string
}

// LinkCatalog writes near/page links for one brand (code) or every brand (""), only for
// families that have no catalog or vendor picture linked yet. Returns links written.
func LinkCatalog(ctx context.Context, pool *pgxpool.Pool, brand string) (int, error) {
	rows, err := pool.Query(ctx, `SELECT id, COALESCE(code,'') FROM brands WHERE ($1='' OR code=$1) AND EXISTS (SELECT 1 FROM catalogs c WHERE c.brand_id=brands.id) ORDER BY 2`, brand)
	if err != nil {
		return 0, err
	}
	type br struct {
		id   int
		code string
	}
	brands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (br, error) {
		var b br
		err := r.Scan(&b.id, &b.code)
		return b, err
	})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, b := range brands {
		n, err := linkBrand(ctx, pool, b.id)
		if err != nil {
			return total, err
		}
		if n > 0 {
			slog.Info("catalog links", "brand", b.code, "links", n)
		}
		total += n
	}
	return total, nil
}

func linkBrand(ctx context.Context, pool *pgxpool.Pool, brandID int) (int, error) {
	// photographs cropped from this brand's books
	cr, err := pool.Query(ctx, `
		SELECT a.id, (a.origin->>'catalog_id')::int, (a.origin->>'page')::int,
		       COALESCE((a.origin->>'x')::float8,0), COALESCE((a.origin->>'y')::float8,0), COALESCE((a.origin->>'w')::float8,0), COALESCE((a.origin->>'h')::float8,0)
		FROM assets a JOIN catalogs c ON c.id=(a.origin->>'catalog_id')::int
		WHERE a.source='catalog' AND c.brand_id=$1 AND NOT a.rejected AND a.dup_of IS NULL
		  AND COALESCE(a.kind, a.origin->>'pkind', 'photo') = 'photo' AND a.origin ? 'page'`, brandID)
	if err != nil {
		return 0, err
	}
	type pageKey struct{ catalog, page int }
	crops := map[pageKey][]cropRow{}
	for cr.Next() {
		var c cropRow
		if err := cr.Scan(&c.asset, &c.catalog, &c.page, &c.box.X, &c.box.Y, &c.box.W, &c.box.H); err != nil {
			cr.Close()
			return 0, err
		}
		if c.box.W <= 0 || c.box.H <= 0 {
			continue
		}
		crops[pageKey{c.catalog, c.page}] = append(crops[pageKey{c.catalog, c.page}], c)
	}
	cr.Close()
	if len(crops) == 0 {
		return 0, nil
	}
	// model numbers printed on those pages
	sr, err := pool.Query(ctx, `
		SELECT k.catalog_id, k.page, k.sku_norm, COALESCE(k.x,0)::float8, COALESCE(k.y,0)::float8, COALESCE(k.w,0)::float8, COALESCE(k.h,0)::float8
		FROM catalog_skus k JOIN catalogs c ON c.id=k.catalog_id WHERE c.brand_id=$1`, brandID)
	if err != nil {
		return 0, err
	}
	skus := map[pageKey][]skuRow{}
	for sr.Next() {
		var s skuRow
		if err := sr.Scan(&s.catalog, &s.page, &s.norm, &s.box.X, &s.box.Y, &s.box.W, &s.box.H); err != nil {
			sr.Close()
			return 0, err
		}
		skus[pageKey{s.catalog, s.page}] = append(skus[pageKey{s.catalog, s.page}], s)
	}
	sr.Close()
	// the brand's items, sorted by normalised SKU for prefix lookups; only families with no
	// catalog or vendor picture linked take new evidence
	ir, err := pool.Query(ctx, `
		SELECT i.id, i.family_id, i.sku_norm FROM items i
		WHERE i.brand_id=$1 AND NOT EXISTS (
		  SELECT 1 FROM asset_links al JOIN assets a ON a.id=al.asset_id
		  WHERE al.family_id=i.family_id AND a.source IN ('catalog','vendor'))
		ORDER BY i.sku_norm`, brandID)
	if err != nil {
		return 0, err
	}
	var items []itemRef
	for ir.Next() {
		var it itemRef
		if err := ir.Scan(&it.id, &it.family, &it.norm); err != nil {
			ir.Close()
			return 0, err
		}
		items = append(items, it)
	}
	ir.Close()
	if len(items) == 0 {
		return 0, nil
	}
	// match returns the items a printed number stands for: the exact SKU, or every SKU that
	// starts with it when the number is long enough to be a model (finish codes follow it)
	match := func(norm string) []itemRef {
		i := sort.Search(len(items), func(i int) bool { return items[i].norm >= norm })
		var out []itemRef
		for j := i; j < len(items) && len(out) < 80; j++ {
			n := items[j].norm
			if n == norm {
				out = append(out, items[j])
				continue
			}
			if len(norm) >= 5 && len(n) > len(norm) && n[:len(norm)] == norm {
				out = append(out, items[j])
				continue
			}
			break
		}
		return out
	}
	type link struct {
		asset, family int64
		item          *int64
		how           string
		weight        float32
	}
	best := map[[2]int64]link{} // asset, family → strongest link
	put := func(l link) {
		k := [2]int64{l.asset, l.family}
		if cur, ok := best[k]; !ok || l.weight > cur.weight {
			best[k] = l
		}
	}
	for pk, cs := range crops {
		ss := skus[pk]
		if len(ss) == 0 {
			continue
		}
		// page extent from everything on it
		var maxX, maxY float64
		boxes := make([]Box, len(cs))
		for i, c := range cs {
			boxes[i] = c.box
			maxX, maxY = math.Max(maxX, c.box.X+c.box.W), math.Max(maxY, c.box.Y+c.box.H)
		}
		models := map[string]bool{}
		for _, s := range ss {
			maxX, maxY = math.Max(maxX, s.box.X+s.box.W), math.Max(maxY, s.box.Y+s.box.H)
			models[s.norm] = true
		}
		diag := math.Hypot(maxX, maxY)
		for _, s := range ss {
			its := match(s.norm)
			if len(its) == 0 {
				continue
			}
			how, idx, w := "near", -1, float32(0)
			if s.box.W > 0 || s.box.H > 0 {
				idx, w = Nearest(s.box, boxes, diag)
			}
			if idx < 0 && len(cs) == 1 && len(models) <= 2 {
				how, idx, w = "page", 0, 0.4
			}
			if idx < 0 {
				continue
			}
			for _, it := range its {
				l := link{asset: cs[idx].asset, family: it.family, how: how, weight: w}
				if it.norm == s.norm {
					id := it.id
					l.item = &id
				} else {
					l.weight *= 0.9 // a prefix match is the model, not this exact SKU
				}
				put(l)
			}
		}
	}
	if len(best) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, l := range best {
		batch.Queue(`INSERT INTO asset_links (asset_id, family_id, item_id, how, weight) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, l.asset, l.family, l.item, l.how, l.weight)
	}
	res := pool.SendBatch(ctx, batch)
	n := 0
	for range best {
		tag, err := res.Exec()
		if err != nil {
			res.Close()
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, res.Close()
}
