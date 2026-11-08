package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"georges/internal/search"
	"georges/internal/taxonomy"
)

// Filter is a browse or search request.
type Filter struct {
	Room       string
	Category   string   // tile/group or leaf name
	Brand      string   // slug
	Tones      []string // silver, gold, bronze, black, white, copper
	PriceMin   int64    // cents, 0 = none
	PriceMax   int64
	Query      string
	Sort       string // "" (relevance/featured) | price-asc | price-desc | new
	Page       int
	PerPage    int
	ShowParts  bool
	WithFacets bool
}

// Card is one product tile.
type Card struct {
	FamilyID   int64
	Brand      string
	BrandSlug  string
	Title      string
	Collection *string
	Category   *string
	PriceCents *int64
	Hash       *string
	W, H       *int
	LQIP       *int
	Grade      int16
	Tones      []string
	Finishes   []string
	SKUs       []string
}

// Facet is one filter value with its count.
type Facet struct {
	Value string
	Label string
	Count int
}

// Result is a page of cards with facets.
type Result struct {
	Cards      []Card
	Total      int
	Page       int
	Pages      int
	PerPage    int
	Intent     search.Intent
	Brands     []Facet
	Categories []Facet
	Tones      []Facet
	// DidYouMean lists the closest products when a search matched nothing.
	DidYouMean []Suggestion
	Took       time.Duration
}

// where collects tagged conditions so facets can rebuild the clause without one of them.
type where struct {
	conds []cond
}

type cond struct {
	tag  string
	sql  string // with $1..$n relative to its own args
	args []any
}

func (w *where) add(tag, sql string, args ...any) {
	w.conds = append(w.conds, cond{tag, sql, args})
}

// build renumbers the parameters, leaving out conditions tagged skip.
func (w *where) build(skip string) (string, []any) {
	var parts []string
	var args []any
	for _, c := range w.conds {
		if skip != "" && c.tag == skip {
			continue
		}
		sql := c.sql
		for i := len(c.args); i >= 1; i-- { // highest first so $1 does not rewrite $10
			sql = strings.ReplaceAll(sql, fmt.Sprintf("$%d", i), fmt.Sprintf("$%d", len(args)+i))
		}
		parts = append(parts, sql)
		args = append(args, c.args...)
	}
	if len(parts) == 0 {
		return "true", args
	}
	return strings.Join(parts, " AND "), args
}

func (s *Store) brandIndex(ctx context.Context) *search.BrandIndex {
	bs, err := s.Brands(ctx)
	if err != nil {
		return nil
	}
	slugs := make([]string, len(bs))
	names := make([]string, len(bs))
	for i, b := range bs {
		slugs[i], names[i] = b.Slug, b.Name
	}
	return search.NewBrandIndex(slugs, names)
}

// Browse runs one filtered, ranked page over search_docs.
func (s *Store) Browse(ctx context.Context, f Filter) (*Result, error) {
	start := time.Now()
	if f.PerPage <= 0 {
		f.PerPage = 48
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	res := &Result{Page: f.Page, PerPage: f.PerPage}
	w := &where{}
	rank := "sd.pop DESC"
	if f.Query != "" {
		in := search.Parse(f.Query, s.brandIndex(ctx))
		res.Intent = in
		if in.Brand != "" && f.Brand == "" {
			f.Brand = in.Brand
		}
		if len(in.Categories) > 0 && f.Category == "" {
			w.add("category", `sd.category = ANY($1)`, in.Categories)
		}
		if in.Finish != nil {
			groups := taxonomy.ToneGroups(in.Finish.Tones)
			if len(groups) > 0 {
				w.add("tones", `sd.tones && $1`, groups)
			}
		}
		var rel []string
		if in.SKU != "" {
			// the lower-cased blob carries every SKU; LIKE on it uses the trigram index, where
			// an unnest over the skus array could not
			// both the printed form and the punctuation-free form, so a partial model number
			// ("K-50") still reaches K50-100-45-PC and its siblings
			w.add("sku", `(sd.blob LIKE $1 OR sd.blob LIKE $2)`, "%"+strings.ToLower(in.SKU)+"%", "%"+strings.ToLower(search.NormSKU(in.SKU))+"%")
			rel = append(rel, fmt.Sprintf(`(CASE WHEN EXISTS (SELECT 1 FROM unnest(sd.skus) k WHERE upper(k) = %s) THEN 10 WHEN sd.blob LIKE %s THEN 3 ELSE 0 END)`, lit(in.SKU), lit("%"+strings.ToLower(search.NormSKU(in.SKU))+"%")))
		}
		if in.Text != "" {
			// full text first, trigram as the fallback for misspellings and OCR'd titles
			w.add("text", `(sd.tsv @@ websearch_to_tsquery('english', $1) OR sd.blob % $2)`, in.Text, strings.ToLower(in.Text))
			rel = append(rel, fmt.Sprintf(`ts_rank_cd(sd.tsv, websearch_to_tsquery('english', %s)) * 4 + similarity(sd.blob, %s)`, lit(in.Text), lit(strings.ToLower(in.Text))))
		}
		if len(rel) > 0 {
			rank = "(" + strings.Join(rel, " + ") + ") DESC, sd.pop DESC"
		}
	}
	if f.Room != "" {
		w.add("room", `sd.room = $1`, f.Room)
	}
	if f.Category != "" {
		w.add("category", `sd.category = ANY($1)`, taxonomy.Resolve(f.Category))
	}
	if f.Brand != "" {
		w.add("brand", `sd.brand_slug = $1`, f.Brand)
	}
	if len(f.Tones) > 0 {
		w.add("tones", `sd.tones && $1`, f.Tones)
	}
	if f.PriceMin > 0 {
		w.add("price", `sd.price_cents >= $1`, f.PriceMin)
	}
	if f.PriceMax > 0 {
		w.add("price", `sd.price_cents <= $1`, f.PriceMax)
	}
	if !f.ShowParts && !strings.EqualFold(f.Category, "Parts") {
		w.add("parts", `sd.tier <> 2`)
	}
	base, bargs := w.build("")
	order := ""
	switch f.Sort {
	case "price-asc":
		order = "sd.price_cents ASC NULLS LAST, "
	case "price-desc":
		order = "sd.price_cents DESC NULLS LAST, "
	}
	// fixtures before parts, pictured before unpictured, trusted pictures before doubtful,
	// then relevance. Without a query, brands take turns two cards at a time (a round-robin
	// by rank within brand) so one maker's price book cannot fill a page. The default orders
	// are precomputed into sd.ord / sd.ord_cat (migration 00012) so a page is an index walk;
	// only searches and price sorts order at request time. family_id breaks ties so pages
	// stay stable.
	bucket := "sd.tier, CASE WHEN sd.grade <= 1 THEN 0 ELSE sd.grade END"
	switch {
	case f.Sort != "":
		order += bucket + ", " + rank + ", sd.family_id"
	case f.Query != "":
		order = bucket + ", " + rank + ", sd.family_id"
	case f.Category != "" && f.Brand == "" && len(taxonomy.Resolve(f.Category)) == 1:
		order = "sd.ord_cat, sd.family_id"
	default:
		order = "sd.ord, sd.family_id"
	}
	args := append(bargs, f.PerPage, (f.Page-1)*f.PerPage)
	q := fmt.Sprintf(`
		SELECT sd.family_id, sd.brand, sd.brand_slug, sd.title, sd.collection, sd.category, sd.price_cents,
		       sd.card_hash, sd.card_w, sd.card_h, sd.card_lqip, sd.grade, sd.tones, sd.finishes, sd.skus[1:4]
		FROM search_docs sd WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`, base, order, len(args)-1, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.FamilyID, &c.Brand, &c.BrandSlug, &c.Title, &c.Collection, &c.Category, &c.PriceCents,
			&c.Hash, &c.W, &c.H, &c.LQIP, &c.Grade, &c.Tones, &c.Finishes, &c.SKUs); err != nil {
			return nil, err
		}
		res.Cards = append(res.Cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// the total is a separate, cached count: a window count would scan every matching row
	// on every page request
	res.Total, err = s.total(ctx, base, bargs)
	if err != nil {
		return nil, err
	}
	res.Pages = (res.Total + f.PerPage - 1) / f.PerPage
	if f.WithFacets {
		if err := s.facets(ctx, f, w, res); err != nil {
			return nil, err
		}
	}
	res.Took = time.Since(start)
	return res, nil
}

// facets counts values for each facet with every other filter applied (the standard
// "disjunctive" facet: the brand list ignores the brand filter). Results are cached briefly
// because a page and its facets are requested together and the catalog changes offline.
func (s *Store) facets(ctx context.Context, f Filter, w *where, res *Result) error {
	key := fmt.Sprintf("%s|%s|%s|%v|%d|%d|%s|%v", f.Room, f.Category, f.Brand, f.Tones, f.PriceMin, f.PriceMax, f.Query, f.ShowParts)
	if c := facetCache.get(key); c != nil {
		res.Brands, res.Categories, res.Tones = c.brands, c.cats, c.tones
		return nil
	}
	var err error
	bw, bargs := w.build("brand")
	res.Brands, err = s.facet(ctx, `SELECT sd.brand_slug, sd.brand, count(*)::int FROM search_docs sd WHERE `+bw+` GROUP BY 1,2 ORDER BY 3 DESC, 2 LIMIT 40`, bargs)
	if err != nil {
		return err
	}
	cw, cargs := w.build("category")
	res.Categories, err = s.facet(ctx, `SELECT sd.category, sd.category, count(*)::int FROM search_docs sd WHERE `+cw+` AND sd.category IS NOT NULL GROUP BY 1,2 ORDER BY 3 DESC, 2 LIMIT 30`, cargs)
	if err != nil {
		return err
	}
	tw, targs := w.build("tones")
	res.Tones, err = s.facet(ctx, `SELECT t, t, count(*)::int FROM search_docs sd, unnest(sd.tones) t WHERE `+tw+` GROUP BY 1,2 ORDER BY 3 DESC`, targs)
	if err != nil {
		return err
	}
	for i := range res.Tones {
		res.Tones[i].Label = taxonomy.ToneLabel(res.Tones[i].Value)
	}
	facetCache.put(key, &facetEntry{brands: res.Brands, cats: res.Categories, tones: res.Tones})
	return nil
}

func (s *Store) facet(ctx context.Context, q string, args []any) ([]Facet, error) {
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("facet: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Facet, error) {
		var f Facet
		err := r.Scan(&f.Value, &f.Label, &f.Count)
		return f, err
	})
}

func lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// total counts a filter's rows, cached by the rendered WHERE clause and its arguments.
func (s *Store) total(ctx context.Context, where string, args []any) (int, error) {
	key := "total|" + where + "|" + fmt.Sprint(args...)
	if c := facetCache.get(key); c != nil {
		return c.total, nil
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM search_docs sd WHERE `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("total: %w", err)
	}
	facetCache.put(key, &facetEntry{total: n})
	return n, nil
}

type facetEntry struct {
	brands, cats, tones []Facet
	total               int
	rooms               map[string]int
	at                  time.Time
}

type facetMemo struct {
	mu sync.Mutex
	m  map[string]*facetEntry
}

var facetCache = &facetMemo{m: map[string]*facetEntry{}}

func (c *facetMemo) get(k string) *facetEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[k]
	// the catalog changes offline (index build), so a half hour of staleness costs nothing
	if e == nil || time.Since(e.at) > 30*time.Minute {
		return nil
	}
	return e
}

func (c *facetMemo) put(k string, e *facetEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 2000 {
		c.m = map[string]*facetEntry{}
	}
	e.at = time.Now()
	c.m[k] = e
}

// RoomCounts returns families per room for the navigation.
func (s *Store) RoomCounts(ctx context.Context) (map[string]int, error) {
	if c := facetCache.get("rooms"); c != nil && c.rooms != nil {
		return c.rooms, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT room, count(*)::int FROM search_docs WHERE room IS NOT NULL AND tier <> 2 GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var r string
		var n int
		if err := rows.Scan(&r, &n); err != nil {
			return nil, err
		}
		out[r] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	facetCache.put("rooms", &facetEntry{rooms: out})
	return out, nil
}

// Suggestion is one typeahead row.
type Suggestion struct {
	Kind     string // family | brand | category
	Label    string
	Sub      string
	URL      string
	Hash     *string
	LQIP     *int
	FamilyID int64
}

// Suggest answers the header typeahead: exact SKU hits first, then title/collection prefix
// matches, then brands and categories whose names match.
func (s *Store) Suggest(ctx context.Context, q string, limit int) ([]Suggestion, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return nil, nil
	}
	lq := strings.ToLower(q)
	var out []Suggestion
	bs, _ := s.Brands(ctx)
	for _, b := range bs {
		if strings.Contains(strings.ToLower(b.Name), lq) && b.Families > 0 {
			out = append(out, Suggestion{Kind: "brand", Label: b.Name, Sub: fmt.Sprintf("%d products", b.Families), URL: "/brand/" + b.Slug})
			if len(out) >= 3 {
				break
			}
		}
	}
	for g := range taxonomy.Groups {
		if strings.Contains(strings.ToLower(g), lq) {
			out = append(out, Suggestion{Kind: "category", Label: g, URL: "/shop?category=" + g})
		}
	}
	if len(lq) < 3 {
		return out, nil
	}
	// the trigram index answers ILIKE; similarity only orders the candidates it finds
	rows, err := s.Pool.Query(ctx, `
		SELECT sd.family_id, sd.title, sd.brand, sd.card_hash, sd.card_lqip,
		       (CASE WHEN EXISTS (SELECT 1 FROM unnest(sd.skus) k WHERE upper(k) LIKE upper($1)||'%') THEN 3 ELSE 0 END
		        + CASE WHEN lower(sd.title) LIKE $2||'%' THEN 2 ELSE 0 END
		        + CASE WHEN lower(coalesce(sd.collection,'')) LIKE $2||'%' THEN 2 ELSE 0 END
		        + similarity(sd.blob, $2) + CASE WHEN sd.grade <= 1 THEN 0.5 ELSE 0 END) AS score
		FROM search_docs sd
		WHERE sd.tier <> 2 AND (sd.blob LIKE '%'||$2||'%' OR sd.blob LIKE '%'||$4||'%')
		ORDER BY score DESC, sd.pop DESC LIMIT $3`, q, lq, limit, strings.ToLower(search.NormSKU(q)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sg Suggestion
		var score float32
		if err := rows.Scan(&sg.FamilyID, &sg.Label, &sg.Sub, &sg.Hash, &sg.LQIP, &score); err != nil {
			return nil, err
		}
		sg.Kind = "family"
		sg.URL = fmt.Sprintf("/p/%d", sg.FamilyID)
		out = append(out, sg)
	}
	return out, rows.Err()
}
