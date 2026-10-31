package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BrowseFilter is the state of the shop sidebar.
type BrowseFilter struct {
	AnyWord    bool   // match any query concept instead of all (set automatically as a fallback)
	AnyFinish  bool   // also admit families whose finish is unknown (set when strict matches are few)
	Intent     Intent // parsed from Query by Browse
	Room       string
	Category   string   // exact element of category_path
	Brands     []string // brand slugs
	Finishes   []string
	MinCents   int64
	MaxCents   int64
	Query      string
	StrictType bool   // type words retrieve by category membership instead of title text (set by Browse when that yields enough)
	Sort       string // relevance | price_asc | price_desc | name
	Page       int
	PerPage    int
}

type Facet struct {
	Name   string
	Values []FacetValue
}

type FacetValue struct {
	Label string
	Value string
	Count int
	On    bool
}

type BrowseResult struct {
	Widened       bool   // query fell back to any-word matching
	FinishWidened bool   // strict finish matches were few; unknown-finish families included too
	Understood    string // "faucets · silver finishes (chrome, nickel, …)"
	Items         []FamilyRow
	Total         int
	Categories    []FacetValue
	Brands        []FacetValue
	Finishes      []FacetValue
	MinCents      int64
	MaxCents      int64
}

// where builds the WHERE clause; skip names a facet whose own filter is omitted (so its counts
// show what *would* match).
func (f BrowseFilter) where(skip string) (string, []any) {
	conds, args := []string{"TRUE"}, []any{}
	add := func(c string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(c, "?", fmt.Sprintf("$%d", len(args))))
	}
	_ = add
	if f.Room != "" {
		add("f.room = ?", f.Room)
	}
	if f.Category != "" && skip != "category" {
		// exact facet value, or a merchandised keyword (tile) matched against category/type/title
		// taxonomy first: the tile/category resolves to curated categories; title text is only
		// consulted for families that have no category at all
		args = append(args, categoriesForFilter(f.Category), categoryPattern(f.Category))
		conds = append(conds, fmt.Sprintf("(f.category_path && $%d OR (COALESCE(array_length(f.category_path,1),0)=0 AND (COALESCE(f.product_type,'') ~* $%d OR COALESCE(f.title,'') ~* $%d)))", len(args)-1, len(args), len(args)))
	}
	if len(f.Brands) > 0 && skip != "brand" {
		add("b.slug = ANY(?)", f.Brands)
	}
	if len(f.Finishes) > 0 && skip != "finish" {
		add("EXISTS (SELECT 1 FROM products p WHERE p.family_id=f.id AND p.finish = ANY(?))", f.Finishes)
	}
	if f.MinCents > 0 && skip != "price" {
		add("(SELECT max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) >= ?", f.MinCents)
	}
	if f.MaxCents > 0 && skip != "price" {
		add("(SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) <= ?", f.MaxCents)
	}
	if !f.Intent.Empty() {
		conds = append(conds, f.Intent.condsStrict(&args, f.AnyWord, f.AnyFinish, f.StrictType)...)
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (s *Store) Browse(ctx context.Context, f BrowseFilter) (*BrowseResult, error) {
	if f.PerPage <= 0 {
		f.PerPage = 24
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	res := &BrowseResult{}
	if q := strings.TrimSpace(f.Query); q != "" {
		f.Intent = ParseIntent(q, s.brandNames(ctx))
		if f.Room != "" {
			f.Intent.Room = "" // the URL room wins; "kitchen faucet" on the kitchen page is not a second filter
		}
		res.Understood = f.Intent.Explain()
	}
	from := browseFrom

	// taxonomy first: when the query names a product type we know categories for, retrieve by
	// category; fall back to title/blob text only when that finds too little
	if f.Intent.HasTypeGroup() {
		f.StrictType = true
		w, args := f.where("")
		var n int
		if err := s.Pool.QueryRow(ctx, "SELECT count(*)"+from+w, args...).Scan(&n); err != nil {
			return nil, err
		}
		if n < 8 {
			f.StrictType = false
		}
	}
	w, args := f.where("")
	if err := s.Pool.QueryRow(ctx, "SELECT count(*), COALESCE(min(pr.lo),0), COALESCE(max(pr.hi),0)"+from+
		" LEFT JOIN LATERAL (SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) lo, max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) hi FROM products p WHERE p.family_id=f.id) pr ON TRUE"+w, args...).
		Scan(&res.Total, &res.MinCents, &res.MaxCents); err != nil {
		return nil, err
	}
	// few families carry the finish explicitly: admit the ones whose finish is unknown as well
	if f.Intent.HasFinish() && !f.AnyFinish && res.Total < f.PerPage {
		f.AnyFinish = true
		w, args = f.where("")
		if err := s.Pool.QueryRow(ctx, "SELECT count(*), COALESCE(min(pr.lo),0), COALESCE(max(pr.hi),0)"+from+
			" LEFT JOIN LATERAL (SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) lo, max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) hi FROM products p WHERE p.family_id=f.id) pr ON TRUE"+w, args...).
			Scan(&res.Total, &res.MinCents, &res.MaxCents); err != nil {
			return nil, err
		}
		res.FinishWidened = true
	}
	// a multi-concept query that finds nothing at all: widen to any concept
	if !f.AnyWord && res.Total == 0 && len(f.Intent.Concepts) > 1 {
		f.AnyWord = true
		w, args = f.where("")
		if err := s.Pool.QueryRow(ctx, "SELECT count(*), COALESCE(min(pr.lo),0), COALESCE(max(pr.hi),0)"+from+
			" LEFT JOIN LATERAL (SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) lo, max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) hi FROM products p WHERE p.family_id=f.id) pr ON TRUE"+w, args...).
			Scan(&res.Total, &res.MinCents, &res.MaxCents); err != nil {
			return nil, err
		}
		res.Widened = true
	}

	// Featured: premier fixtures before parts, families with a photo first (sharp ≥400 px ones,
	// i.e. HD or large native crops, ahead of small ones), then a stable per-family shuffle so
	// the first page mixes brands instead of walking the alphabet
	bucket := "f.tier, (fb.photo_path IS NULL)"
	rel := ""
	if len(f.Intent.Concepts) > 0 {
		rel = "(" + f.Intent.relevance(&args) + ") DESC, "
	}
	// inside a tile, the families that *are* the category lead (see categoryCore)
	if core := CategoryCore(f.Category); f.Category != "" && core != "" {
		args = append(args, `\m(`+core+`)`)
		rel += fmt.Sprintf("(COALESCE(f.title,'') ~* $%d) DESC, ", len(args))
	}
	// round-robin inside a bucket: a brand gets 2 slots per round and a brand+title 1, so one
	// book cannot flood a page (Brizo alone prints 2,400 families) and pagination stays
	// consistent; sharp (≥400 px) photos lead each round
	order := rel + bucket + ", GREATEST((row_number() OVER (PARTITION BY " + bucket + ", f.brand_id ORDER BY (COALESCE(fb.photo_w,0) < 400), hashtext(f.id::text)) - 1) / 2," +
		" row_number() OVER (PARTITION BY " + bucket + ", f.brand_id, lower(f.title) ORDER BY (COALESCE(fb.photo_w,0) < 400), hashtext(f.id::text)) - 1)," +
		" (COALESCE(fb.photo_w,0) < 400), hashtext(f.id::text)"
	switch f.Sort {
	case "price_asc":
		order = "(SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) NULLS LAST, f.title"
	case "price_desc":
		order = "(SELECT max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) DESC NULLS LAST, f.title"
	case "name":
		order = "f.title, b.name"
	}
	pargs := append(append([]any{}, args...), f.PerPage, (f.Page-1)*f.PerPage)
	rows, err := s.Pool.Query(ctx, familySelect+w+" ORDER BY "+order+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(pargs)-1, len(pargs)), pargs...)
	if err != nil {
		return nil, err
	}
	if res.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[FamilyRow]); err != nil {
		return nil, err
	}
	// when the shopper asked for a finish, show each family in that finish where a crop exists
	// and say which finish the picture shows; otherwise the finish label is noise
	if len(f.Finishes) == 0 && !f.Intent.HasFinish() {
		for i := range res.Items {
			res.Items[i].ImageFinish = ""
		}
	} else {
		var rx string
		for _, c := range f.Intent.Concepts {
			if c.Kind == "finish" {
				rx = regexAlt(c.Patterns)
				break
			}
		}
		_ = s.preferFinishImages(ctx, res.Items, f.Finishes, rx)
	}
	// sister families extracted from the same price-book line share one crop and title; show
	// each picture once per page
	seen := map[string]bool{}
	kept := res.Items[:0]
	for _, it := range res.Items {
		k := it.Image + "|" + it.Title
		if it.Image != "" && seen[k] {
			continue
		}
		seen[k] = true
		kept = append(kept, it)
	}
	res.Items = kept

	// facets
	cw, cargs := f.where("category")
	res.Categories, err = s.facet(ctx, "SELECT c, c, count(*)::int"+from+", unnest(f.category_path) c"+cw+" AND c NOT IN ('None','') GROUP BY c ORDER BY 3 DESC, 1 LIMIT 40", cargs, func(v string) bool { return v == f.Category })
	if err != nil {
		return nil, err
	}
	bw, bargs := f.where("brand")
	res.Brands, err = s.facet(ctx, "SELECT b.name, b.slug, count(*)::int"+from+bw+" GROUP BY b.name, b.slug ORDER BY 3 DESC, 1 LIMIT 60", bargs, func(v string) bool { return contains(f.Brands, v) })
	if err != nil {
		return nil, err
	}
	fw, fargs := f.where("finish")
	res.Finishes, err = s.facet(ctx, "SELECT p.finish, p.finish, count(DISTINCT f.id)::int"+from+" JOIN products p ON p.family_id=f.id"+fw+" AND p.finish IS NOT NULL AND p.finish<>'' GROUP BY p.finish ORDER BY 3 DESC, 1 LIMIT 40", fargs, func(v string) bool { return contains(f.Finishes, v) })
	return res, err
}

func (s *Store) facet(ctx context.Context, sql string, args []any, on func(string) bool) ([]FacetValue, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FacetValue
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Label, &v.Value, &v.Count); err != nil {
			return nil, err
		}
		v.On = on(v.Value)
		out = append(out, v)
	}
	return out, rows.Err()
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// RoomCounts returns family counts per room for the header/landing.
func (s *Store) RoomCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT COALESCE(room,''), count(*)::int FROM product_families GROUP BY 1`)
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
	return out, rows.Err()
}

// ReassignRooms recomputes room for every family (after keyword changes).
func (s *Store) ReassignRooms(ctx context.Context) (int, error) {
	// catalog-sourced families also carry the section heading of the page they came from
	rows, err := s.Pool.Query(ctx, `
		SELECT f.id, f.category_path, COALESCE(f.product_type,''),
		  COALESCE(f.title,'') || ' ' || COALESCE((
		    SELECT string_agg(sec, ' ') FROM (
		      SELECT DISTINCT COALESCE((SELECT t.title FROM catalog_toc t WHERE t.catalog_id=p.catalog_id AND t.page<=p.catalog_page AND t.source='printed' AND t.level=1 ORDER BY t.page DESC LIMIT 1),'')
		        || ' ' || COALESCE((SELECT t.title FROM catalog_toc t WHERE t.catalog_id=p.catalog_id AND t.page<=p.catalog_page AND t.source='ocr' ORDER BY t.page DESC LIMIT 1),'')
		        || ' ' || COALESCE((SELECT c.title FROM catalogs c WHERE c.id=p.catalog_id),'') AS sec
		      FROM products p WHERE p.family_id=f.id AND p.catalog_id IS NOT NULL LIMIT 3) x), ''),
		  b.name
		FROM product_families f JOIN brands b ON b.id=f.brand_id`)
	if err != nil {
		return 0, err
	}
	type fam struct {
		id    int64
		cats  []string
		ptype string
		title string
		brand string
	}
	var fams []fam
	for rows.Next() {
		var x fam
		if err := rows.Scan(&x.id, &x.cats, &x.ptype, &x.title, &x.brand); err != nil {
			rows.Close()
			return 0, err
		}
		fams = append(fams, x)
	}
	rows.Close()
	n := 0
	for _, x := range fams {
		if _, err := s.Pool.Exec(ctx, `UPDATE product_families SET room=COALESCE(NULLIF($2,''), room) WHERE id=$1`, x.id, RoomFor(x.cats, x.ptype, x.title, x.brand)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// preferFinishImages swaps each family's card image for a crop linked to a SKU in one of the
// wanted finishes (exact names from the facet, or a regex from the query), when one exists.
func (s *Store) preferFinishImages(ctx context.Context, items []FamilyRow, exact []string, rx string) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if rx == "" {
		rx = "^$"
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (fc.family_id) fc.family_id, COALESCE(cc.hd_path, cc.path), COALESCE(cc.hd_w, cc.w), p.finish
		FROM family_crops fc
		JOIN catalog_crops cc ON cc.id=fc.crop_id AND cc.kind='photo'
		JOIN products p ON p.family_id=fc.family_id AND (p.sku=cc.sku OR (cc.base_n IS NOT NULL AND p.sku_n=cc.base_n))
		WHERE fc.family_id = ANY($1) AND COALESCE(p.finish,'')<>'' AND (p.finish = ANY($2) OR p.finish ~* $3)
		  AND COALESCE(cc.quality,0) >= 0.3 AND cc.w >= 120 AND cc.h >= 120
		ORDER BY fc.family_id, COALESCE(cc.quality,0.3) DESC, cc.w*cc.h DESC`, ids, exact, rx)
	if err != nil {
		return err
	}
	defer rows.Close()
	best := map[int64][3]string{}
	for rows.Next() {
		var id int64
		var path, finish string
		var w int
		if err := rows.Scan(&id, &path, &w, &finish); err != nil {
			return err
		}
		best[id] = [3]string{path, fmt.Sprint(w), finish}
	}
	for i := range items {
		if b, ok := best[items[i].ID]; ok {
			items[i].Image = "/files/" + b[0]
			items[i].ImageW = atoiDefault(b[1])
			items[i].ImageFinish = b[2]
		}
	}
	return nil
}

func atoiDefault(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// browseFrom is the relation every browse count and page runs over.
const browseFrom = " FROM product_families f JOIN brands b ON b.id=f.brand_id LEFT JOIN family_best fb ON fb.family_id=f.id"

// CountFamilies returns how many families a room/category/brand/finish/price filter matches:
// the number its shop page would show. A free-text Query is not parsed here; use Browse.
func (s *Store) CountFamilies(ctx context.Context, f BrowseFilter) (int, error) {
	w, args := f.where("")
	var n int
	err := s.Pool.QueryRow(ctx, "SELECT count(*)"+browseFrom+w, args...).Scan(&n)
	return n, err
}
