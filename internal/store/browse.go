package store

import (
	"context"
	"fmt"
	"os"
	"sort"
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
	Specs      map[string][]string // attribute facets: key → any of these values ("Mounting" → Wall Mount)
	MinCents   int64
	MaxCents   int64
	Query      string
	Exact      bool   // search the words as typed; no spelling correction
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
	Widened       bool     // query fell back to any-word matching
	FinishWidened bool     // strict finish matches were few; unknown-finish families included too
	Original      string   // the query as typed, when Corrected differs
	Corrected     string   // the query after spelling correction ("brused nickle" → "brushed nickel")
	Understood    string   // "faucets · silver finishes (chrome, nickel, …)"
	FinishNames   []string // real finish names the query resolved to (chips under the title)
	Items         []FamilyRow
	Total         int
	Categories    []FacetValue
	Brands        []FacetValue
	Finishes      []FacetValue
	Specs         []Facet // attribute facets worth showing for this result set (specFacetKeys order)
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
	if len(f.Specs) > 0 && skip != "spec" {
		keys := make([]string, 0, len(f.Specs))
		for k := range f.Specs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var ors []string
			for _, v := range f.Specs[k] {
				args = append(args, k, v)
				ors = append(ors, fmt.Sprintf("f.attributes @> jsonb_build_object($%d::text, $%d::text)", len(args)-1, len(args)))
			}
			if len(ors) > 0 {
				conds = append(conds, "("+strings.Join(ors, " OR ")+")")
			}
		}
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
		f.Intent = ParseIntentDict(q, s.brandNames(ctx), s.finishDict(ctx))
		if !f.Exact {
			if fixed := s.correctQuery(ctx, q, f.Intent); fixed != "" && fixed != q {
				res.Original, res.Corrected = q, fixed
				f.Query = fixed
				f.Intent = ParseIntentDict(fixed, s.brandNames(ctx), s.finishDict(ctx))
			}
		}
		if f.Room != "" {
			f.Intent.Room = "" // the URL room wins; "kitchen faucet" on the kitchen page is not a second filter
		}
		res.Understood = f.Intent.Explain()
		if fc := f.Intent.FinishConcept(); fc != nil {
			res.FinishNames = fc.Names
		}
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

	ids, err := s.rankFamilies(ctx, f, w, args)
	if err != nil {
		return nil, err
	}
	if res.Items, err = s.familiesByIDs(ctx, ids); err != nil {
		return nil, err
	}
	// when the shopper asked for a finish, show each family in that finish where a crop exists
	// and say which finish the picture shows; otherwise the finish label is noise
	if len(f.Finishes) == 0 && !f.Intent.HasFinish() {
		for i := range res.Items {
			res.Items[i].ImageFinish = ""
		}
	} else {
		_ = s.preferFinishImages(ctx, res.Items, f.Finishes, f.Intent.FinishConcept())
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
	if err != nil {
		return nil, err
	}
	res.Specs, err = s.specFacets(ctx, f, from, res.Total)
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

// rankFamilies orders every family the filter matches and returns one page of ids. Ranking
// runs over a slim projection (relevance, tier, image fit, brand, a stable hash) so the heavy
// card columns are fetched for 24 rows only.
//
// Featured order: relevance (type/category hits, the finish asked for, fixtures before parts),
// families with any photo before none, then a brand round-robin so no book floods a page: a
// finish query gives every brand one slot per round, other pages two, and a repeated title
// inside a brand waits three rounds. Inside a brand the families whose picture shows the
// finish asked for come first (a vendor master in it, or a crop whose metal reads as that
// tone), then sharp (≥400 px) photos.
func (s *Store) rankFamilies(ctx context.Context, f BrowseFilter, where string, args []any) ([]int64, error) {
	finishAsked := len(f.Finishes) > 0
	if fc := f.Intent.FinishConcept(); fc != nil && len(fc.Names) > 0 {
		finishAsked = true
	}
	rel := "0"
	if len(f.Intent.Concepts) > 0 {
		// a finish query ranks the picture before the tier; parts (tier 2) stay last
		rel = f.Intent.relevanceOpt(&args, !finishAsked)
	}
	parts := "f.tier"
	if finishAsked && !f.Intent.WantsParts() {
		parts = "(f.tier = 2)"
	}
	core := "FALSE"
	if c := CategoryCore(f.Category); f.Category != "" && c != "" {
		args = append(args, `\m(`+c+`)`)
		core = fmt.Sprintf("(COALESCE(f.title,'') ~* $%d)", len(args))
	}
	fit, xact, slots := "0", "FALSE", 2
	if fc := f.Intent.FinishConcept(); fc != nil && len(fc.Names) > 0 {
		slots = 1
		fit = imageFit(&args, fc.Names, fc.Groups)
		if len(fc.Exact) > 0 {
			args = append(args, fc.Exact)
			xact = fmt.Sprintf("EXISTS (SELECT 1 FROM products p WHERE p.family_id=f.id AND p.finish = ANY($%d))", len(args))
		}
	} else if len(f.Finishes) > 0 {
		slots = 1
		fit = imageFit(&args, f.Finishes, s.finishDict(ctx).groupsFor(f.Finishes))
	}
	// brand balance first, then the picture: every round seats each brand's best-fitting
	// family, so one book with bronze photographs of everything (Emtek) cannot fill the page.
	// A family whose only picture shows another metal (fit -1) waits behind every round.
	order := fmt.Sprintf("rel DESC, core DESC, parts, nophoto, (fit < 0), GREATEST((rb-1)/%d, (rt-1)*3), fit DESC, xact DESC, tier, small, h", slots)
	if finishAsked {
		// a finish page never opens with a picture of another metal, whatever the type or
		// category score ("brushed nickel faucet": a category hit in a brass photograph waits)
		order = fmt.Sprintf("(fit < 0), rel DESC, core DESC, parts, nophoto, GREATEST((rb-1)/%d, (rt-1)*3), fit DESC, xact DESC, tier, small, h", slots)
	}
	switch f.Sort {
	case "price_asc":
		order = "lo NULLS LAST, title"
	case "price_desc":
		order = "hi DESC NULLS LAST, title"
	case "name":
		order = "title, brand"
	}
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	q := fmt.Sprintf(`WITH r AS (
		  SELECT f.id, (%s) AS rel, %s AS core, f.tier, %s AS parts, (fb.photo_path IS NULL) AS nophoto, (%s) AS fit, (%s) AS xact,
		    f.brand_id, b.name AS brand, lower(COALESCE(f.title,'')) AS title,
		    (COALESCE(fb.photo_w,0) < 400) AS small, hashtext(f.id::text) AS h,
		    (SELECT min(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) AS lo,
		    (SELECT max(our_cents(p.source,p.price_cents,p.msrp_cents,b.price_multiplier)) FROM products p WHERE p.family_id=f.id) AS hi
		  %s%s),
		o AS (
		  SELECT r.*,
		    row_number() OVER (PARTITION BY rel, core, parts, nophoto, (fit < 0), brand_id ORDER BY fit DESC, xact DESC, tier, small, h) AS rb,
		    row_number() OVER (PARTITION BY rel, core, parts, nophoto, (fit < 0), brand_id, title ORDER BY fit DESC, xact DESC, tier, small, h) AS rt
		  FROM r)
		SELECT id, rel, tier, nophoto, fit, xact, GREATEST((rb-1)/%d, (rt-1)*3) AS round, brand FROM o ORDER BY %s LIMIT $%d OFFSET $%d`,
		rel, core, parts, fit, xact, browseFrom, where, slots, order, len(args)-1, len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	type ranked struct {
		ID      int64
		Rel     int
		Tier    int
		NoPhoto bool
		Fit     int
		Exact   bool
		Round   int
		Brand   string
	}
	rs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ranked])
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rs))
	for _, r := range rs {
		if os.Getenv("RANK_DEBUG") != "" {
			fmt.Printf("rank %d rel=%d tier=%d nophoto=%v fit=%d exact=%v round=%d %s\n", r.ID, r.Rel, r.Tier, r.NoPhoto, r.Fit, r.Exact, r.Round, r.Brand)
		}
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// imageFit is the SQL for how well a family's pictures fit the finish asked for: 2 when a
// vendor master of a SKU in that finish exists, 1 when some crop of the family (or the
// picture shown) reads as the asked tone, 0 when the tone of the shown picture is unknown,
// -1 when the only picture shows another metal (a brass mirror on a brushed-nickel page).
func imageFit(args *[]any, names, groups []string) string {
	*args = append(*args, names, groups)
	n, g := len(*args)-1, len(*args)
	return fmt.Sprintf(`(CASE
		  WHEN EXISTS (SELECT 1 FROM product_images pi JOIN products p ON p.id=pi.product_id
		               WHERE p.family_id=f.id AND p.finish = ANY($%[1]d) AND pi.path IS NOT NULL AND pi.path NOT LIKE 'miss:%%') THEN 2
		  WHEN EXISTS (SELECT 1 FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id
		               WHERE fc.family_id=f.id AND cc.kind='photo' AND cc.vtone = ANY($%[2]d) AND COALESCE(cc.vtonescore,0) >= 0.5
		                 AND COALESCE(cc.quality,0) >= 0.3 AND cc.w >= 120 AND cc.h >= 120) THEN 1
		  WHEN fb.photo_tone IS NULL OR fb.photo_tone='other' THEN 0
		  WHEN fb.photo_tone = ANY($%[2]d) THEN 1
		  ELSE -1 END)`, n, g)
}

// familiesByIDs hydrates card rows in the given order.
func (s *Store) familiesByIDs(ctx context.Context, ids []int64) ([]FamilyRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, familySelect+" WHERE f.id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[FamilyRow])
	if err != nil {
		return nil, err
	}
	by := make(map[int64]FamilyRow, len(got))
	for _, r := range got {
		by[r.ID] = r
	}
	out := make([]FamilyRow, 0, len(ids))
	for _, id := range ids {
		if r, ok := by[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// groupsFor folds facet finish names into the tones a photograph shows.
func (d *FinishDict) groupsFor(names []string) []string {
	var tones []string
	for _, n := range names {
		if e := d.Entry(n); e != nil {
			tones = append(tones, e.Tones...)
		}
	}
	return ToneGroups(tones)
}

// preferFinishImages swaps each family's card image for one that shows the finish asked for
// (exact names from the facet, or the resolved names and tones of the query): a vendor master
// of a SKU in that finish, then a catalog crop linked to such a SKU, then any crop of the
// family whose metal the vision pass read as that tone. ImageMatch records what the card
// shows: exact (the finish itself), tone (the right colour of metal), other (a different
// tone, named in ImageTone) or "" (unknown).
func (s *Store) preferFinishImages(ctx context.Context, items []FamilyRow, exact []string, fc *Concept) error {
	if len(items) == 0 {
		return nil
	}
	dict := s.finishDict(ctx)
	names, groups := exact, []string(nil)
	if fc != nil {
		names = append(append([]string{}, names...), fc.Names...)
		groups = fc.Groups
	}
	if len(exact) > 0 {
		groups = append(groups, dict.groupsFor(exact)...)
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (family_id) family_id, path, w, finish, how FROM (
		  SELECT p.family_id, pi.path, COALESCE(pi.width,0) AS w, p.finish, 'exact' AS how, 3.0 AS score
		  FROM product_images pi JOIN products p ON p.id=pi.product_id
		  WHERE p.family_id = ANY($1) AND p.finish = ANY($2) AND pi.path IS NOT NULL AND pi.path NOT LIKE 'miss:%'
		  UNION ALL
		  SELECT fc.family_id, COALESCE(cc.hd_path, cc.path), COALESCE(cc.hd_w, cc.w), p.finish, 'exact', 2 + COALESCE(cc.quality,0.3)
		  FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id AND cc.kind='photo'
		  JOIN products p ON p.family_id=fc.family_id AND (p.sku=cc.sku OR (cc.base_n IS NOT NULL AND p.sku_n=cc.base_n))
		  WHERE fc.family_id = ANY($1) AND p.finish = ANY($2) AND COALESCE(cc.quality,0) >= 0.3 AND cc.w >= 120 AND cc.h >= 120
		    -- a price book prints one photo per model whatever the SKU's finish: the camera has the last word
		    AND (cc.vtone IS NULL OR cc.vtone='other' OR COALESCE(cc.vtonescore,0) < 0.5 OR cc.vtone = ANY($3))
		  UNION ALL
		  SELECT fc.family_id, COALESCE(cc.hd_path, cc.path), COALESCE(cc.hd_w, cc.w), '', 'tone', 1 + COALESCE(cc.quality,0.3)
		  FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id AND cc.kind='photo'
		  WHERE fc.family_id = ANY($1) AND cc.vtone = ANY($3) AND COALESCE(cc.vtonescore,0) >= 0.5
		    AND COALESCE(cc.quality,0) >= 0.3 AND cc.w >= 120 AND cc.h >= 120
		) x ORDER BY family_id, score DESC, w DESC`, ids, names, groups)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pick struct {
		path, finish, how string
		w                 int
	}
	best := map[int64]pick{}
	for rows.Next() {
		var id int64
		var p pick
		if err := rows.Scan(&id, &p.path, &p.w, &p.finish, &p.how); err != nil {
			return err
		}
		best[id] = p
	}
	inGroups := func(t string) bool {
		for _, g := range groups {
			if g == t {
				return true
			}
		}
		return false
	}
	for i := range items {
		it := &items[i]
		if p, ok := best[it.ID]; ok {
			it.Image = "/files/" + p.path
			it.ImageW = p.w
			it.ImageFinish = p.finish
			it.ImageMatch = p.how
			if p.how == "tone" {
				it.ImageTone = ""
			}
			continue
		}
		if it.ImageFinish != "" && it.ImageTone == "" {
			// the shown crop belongs to a SKU of a known finish: that finish's tone
			if e := dict.Entry(it.ImageFinish); e != nil && e.Head != "" {
				it.ImageTone = ToneGroup(e.Head)
			}
		}
		switch {
		case it.ImageTone != "" && it.ImageTone != "other" && !inGroups(it.ImageTone):
			it.ImageMatch = "other" // whatever SKU the crop is linked to, it shows another metal
		case it.ImageFinish != "" && contains(names, it.ImageFinish):
			it.ImageMatch = "exact"
		case it.ImageTone != "" && inGroups(it.ImageTone):
			it.ImageMatch = "tone"
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

// specFacetKeys are the attribute keys offered as sidebar facets, in display order; a key is
// shown when the result set carries it on enough families with at least two values.
var specFacetKeys = []string{"Mounting", "Faucet Holes", "Centers", "Spout Reach", "Spout Height", "Material", "Flow Rate", "Valve Type",
	"Handle Style", "Handles", "Spout Type", "Function", "Bowls", "Drain Location", "Overflow", "ADA Compliant", "Cartridge",
	"Handing", "Keying", "Gauge", "Width", "Length", "Height", "Nominal Size", "Door Thickness", "Backset"}

// specFacets counts the attribute values among the families the filter matches (ignoring the
// spec filters themselves, so each facet shows what would match) and keeps the keys worth a
// facet: two or more values, covering at least 3 % of the results, up to eight keys and eight
// values each. Unilog's category pages stop at brand and price; a rep narrowing to wall-mount,
// 8" centers or 1.2 gpm has only the spec sheets otherwise.
func (s *Store) specFacets(ctx context.Context, f BrowseFilter, from string, total int) ([]Facet, error) {
	if total == 0 {
		return nil, nil
	}
	w, args := f.where("spec")
	args = append(args, specFacetKeys)
	rows, err := s.Pool.Query(ctx, "SELECT e.key, e.value, count(*)::int FROM (SELECT f.attributes"+from+w+") x, jsonb_each_text(x.attributes) e"+
		fmt.Sprintf(" WHERE e.key = ANY($%d) AND e.value <> '' GROUP BY 1, 2 HAVING count(*) >= 2 ORDER BY 1, 3 DESC, 2", len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	by := map[string][]FacetValue{}
	for rows.Next() {
		var k, v string
		var n int
		if err := rows.Scan(&k, &v, &n); err != nil {
			return nil, err
		}
		if len(by[k]) < 8 {
			by[k] = append(by[k], FacetValue{Label: v, Value: v, Count: n, On: contains(f.Specs[k], v)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	floor := max(3, total*3/100)
	var out []Facet
	for _, k := range specFacetKeys {
		vals := by[k]
		sum := 0
		for _, v := range vals {
			sum += v.Count
		}
		if len(f.Specs[k]) == 0 && (len(vals) < 2 || sum < floor) {
			continue
		}
		if len(vals) == 0 {
			continue
		}
		out = append(out, Facet{Name: k, Values: vals})
		if len(out) >= 8 {
			break
		}
	}
	return out, nil
}
