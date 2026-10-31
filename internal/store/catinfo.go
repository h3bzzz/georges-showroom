package store

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"georges_/internal/catinfo"
)

// Catalog information mining. For every family with SKU rows in a price book this finds
// the block of the page that belongs to it (its rows, the header above them and the feature
// text around them, bounded by the rows of other families), runs the catinfo parsers over
// that block and records the facts in family_facts. The facts then fill product_families
// attributes/collection/description/bullets where the site left them empty, and the finish
// column of catalog SKUs that still have none.

// InfoStats summarises one run.
type InfoStats struct {
	Families, WithFacts, Facts, Collections, Bullets, Descriptions int
	FinishesSet                                                    int
	ByKey                                                          map[string]int
}

// InfoSample is one family's extraction, for --dump.
type InfoSample struct {
	FamilyID int64
	Brand    string
	Title    string
	Catalog  int
	Page     int
	Result   catinfo.Result
	Block    []string
}

type infoItem struct {
	sku, line        string
	x, y             int
	productID        int64
	familyID         int64
	finish           string
	title, room, cat string
}

type infoFam struct {
	id        int64
	brandID   int
	brandCode string
	brandName string
	title     string
	room, cat string
	source    string
	skus      map[string]bool
	pages     map[pageKey]bool
	facts     map[string]catinfo.Fact
	factSrc   map[string][2]int // catalog, page of the winning fact
	coll      string
	collConf  float64
	desc      string
	bullets   []string
	finishes  []string
	rowFinish map[int64]string // product id → finish named on its own row
	famFinish string           // the one finish the block names
}

// reSibKey reduces a SKU to the part shared by its finish/handle variants.
var reSibKey = regexp.MustCompile(`^([A-Z0-9]{4,}[A-Z0-9]*?)(?:[-/.][A-Z0-9]{1,6})*$`)

func sibKey(sku string) string {
	u := strings.ToUpper(sku)
	if i := strings.IndexAny(u, "-/."); i >= 4 {
		return u[:i]
	}
	if len(u) > 6 {
		return u[:6]
	}
	return u
}

// InfoOpts selects what ExtractCatalogInfo works on.
type InfoOpts struct {
	Brand   string // brand code, "" = all
	Catalog int    // one catalog id, 0 = all (debugging)
	Family  int64  // one family id for --dump, 0 = any
	Dry     bool
	Dump    int
}

// ExtractCatalogInfo runs the catalog parsers for one brand (or all) and writes
// family_facts, merges them into product_families and fills products.finish. With Dry,
// nothing is written. Dump > 0 returns that many sample extractions.
func (s *Store) ExtractCatalogInfo(ctx context.Context, dataDir string, o InfoOpts) (InfoStats, []InfoSample, error) {
	brandCode, dry, dump := o.Brand, o.Dry, o.Dump
	st := InfoStats{ByKey: map[string]int{}}
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, b.id, b.name, c.name, i.page, i.sku, i.line, COALESCE(i.x,0), COALESCE(i.y,0),
		  COALESCE(p.id,0), COALESCE(p.family_id,0), COALESCE(p.finish,''), COALESCE(f.title,''), COALESCE(f.room,''),
		  COALESCE(f.category_path[1],''), COALESCE(f.source,'')
		FROM catalog_items i
		JOIN catalogs c ON c.id=i.catalog_id
		JOIN brands b ON b.code=c.brand_code
		LEFT JOIN products p ON p.brand_id=b.id AND p.sku=i.sku
		LEFT JOIN product_families f ON f.id=p.family_id
		WHERE ($1='' OR c.brand_code=$1) AND ($2=0 OR c.id=$2)
		ORDER BY c.id, i.page, i.y, i.x`, brandCode, o.Catalog)
	if err != nil {
		return st, nil, err
	}
	pages := map[pageKey]*pageData{}
	var pageOrder []pageKey
	fams := map[int64]*infoFam{}
	for rows.Next() {
		var pd pageData
		var it infoItem
		var bid int
		var src string
		if err := rows.Scan(&pd.cat, &pd.bcode, &bid, &pd.bname, &pd.cname, &pd.page, &it.sku, &it.line, &it.x, &it.y,
			&it.productID, &it.familyID, &it.finish, &it.title, &it.room, &it.cat, &src); err != nil {
			rows.Close()
			return st, nil, err
		}
		k := pageKey{pd.cat, pd.page}
		p := pages[k]
		if p == nil {
			pd.items = nil
			p = &pd
			pages[k] = p
			pageOrder = append(pageOrder, k)
		}
		p.items = append(p.items, it)
		if it.familyID != 0 {
			f := fams[it.familyID]
			if f == nil {
				f = &infoFam{id: it.familyID, brandID: bid, brandCode: pd.bcode, brandName: pd.bname, title: it.title, room: it.room, cat: it.cat, source: src,
					skus: map[string]bool{}, pages: map[pageKey]bool{}, facts: map[string]catinfo.Fact{}, factSrc: map[string][2]int{}, rowFinish: map[int64]string{}}
				fams[it.familyID] = f
			}
			f.skus[strings.ToUpper(it.sku)] = true
			f.pages[k] = true
		}
	}
	rows.Close()
	st.Families = len(fams)

	toc, err := s.loadTOC(ctx, brandCode)
	if err != nil {
		return st, nil, err
	}
	codes, err := s.loadFinishCodes(ctx)
	if err != nil {
		return st, nil, err
	}

	var samples []InfoSample
	cache := map[string][]pageLine{}
	for _, k := range pageOrder {
		p := pages[k]
		path := filepath.Join(dataDir, "catalogs", p.bcode, safeDirName(p.cname), fmt.Sprintf("%04d.words.json", p.page))
		lines, ok := cache[path]
		if !ok {
			lines, _ = loadPageLines(path)
			lines = readingOrder(lines)
			if len(cache) > 300 {
				cache = map[string][]pageLine{}
			}
			cache[path] = lines
		}
		if len(lines) == 0 {
			continue
		}
		heading := pageHeading(lines, p.bname)
		tocs := toc.entries(p.cat, p.page)
		var pageSKUs []string
		for _, it := range p.items {
			pageSKUs = append(pageSKUs, it.sku)
		}
		// which line holds each item
		itemLine := make([]int, len(p.items))
		for i, it := range p.items {
			itemLine[i] = findItemLine(lines, it)
		}
		// runs: SKU rows spaced like a table belong together
		runOf := assignRuns(p.items, lines, itemLine)
		byFam := map[int64][]int{}
		for i, it := range p.items {
			if it.familyID != 0 {
				byFam[it.familyID] = append(byFam[it.familyID], i)
			}
		}
		for fid, idx := range byFam {
			f := fams[fid]
			minY, maxY := 1<<30, -1
			xs := map[int]bool{}
			runs := map[int]bool{}
			ownIdx := map[int]bool{}
			keys := map[string]bool{}
			for _, i := range idx {
				it := p.items[i]
				minY, maxY = min(minY, it.y), max(maxY, it.y)
				xs[it.x] = true
				runs[runOf[i]] = true
				if itemLine[i] >= 0 {
					ownIdx[itemLine[i]] = true
				}
				keys[sibKey(it.sku)] = true
			}
			top, bottom := minY-900, maxY+700
			otherOwn := map[int]bool{}
			for i, it := range p.items {
				if it.familyID == 0 || it.familyID == fid {
					continue
				}
				if itemLine[i] >= 0 {
					otherOwn[itemLine[i]] = true
				}
				if runs[runOf[i]] || keys[sibKey(it.sku)] {
					continue // same table run or a finish sibling: not a boundary
				}
				near := false
				for x := range xs {
					if abs(x-it.x) <= 150 {
						near = true
					}
				}
				if !near {
					continue
				}
				if it.y < minY && it.y > top {
					top = it.y
				}
				if it.y > maxY && it.y < bottom {
					bottom = it.y
				}
			}
			var block, own []string
			for li, l := range lines {
				if ownIdx[li] {
					own = append(own, l.Text)
				}
				if l.Y <= top || l.Y >= bottom || otherOwn[li] {
					continue
				}
				block = append(block, l.Text)
			}
			var skus []string
			for s := range f.skus {
				skus = append(skus, s)
			}
			sort.Strings(skus)
			in := catinfo.Input{Brand: f.brandCode, BrandName: f.brandName, Title: f.title, Room: f.room, Category: f.cat, SKUs: skus,
				Own: own, Block: block, Heading: heading, TOC: tocs, PageSKUs: pageSKUs, Spread: len(f.pages)}
			r := catinfo.Extract(in)
			f.merge(r, p.cat, p.page)
			// finishes named on each product's own row
			for _, i := range idx {
				it := p.items[i]
				if it.productID == 0 || it.finish != "" {
					continue
				}
				if fin := rowFinishFor(it, lines, itemLine[i], codes[f.brandCode], codes["*"]); fin != "" {
					if cur, ok := f.rowFinish[it.productID]; !ok || cur == fin {
						f.rowFinish[it.productID] = fin
					} else {
						f.rowFinish[it.productID] = "?" // two rows disagree
					}
				}
			}
			if len(samples) < dump && (len(r.Facts) > 0 || r.Collection != "") && (o.Family == 0 || o.Family == fid) {
				samples = append(samples, InfoSample{FamilyID: fid, Brand: f.brandCode, Title: f.title, Catalog: p.cat, Page: p.page, Result: r, Block: block})
			}
		}
	}

	// ---- write ----
	ids := make([]int64, 0, len(fams))
	for id := range fams {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var finishUpdates [][2]any
	finishByFam := map[int64]map[string]bool{}
	for _, id := range ids {
		f := fams[id]
		for pid, fin := range f.rowFinish {
			if fin != "?" {
				finishUpdates = append(finishUpdates, [2]any{pid, fin})
			}
		}
		if f.famFinish != "" {
			// the block names exactly one finish and no row names another: it applies to every SKU
			for _, fin := range f.rowFinish {
				if fin == "?" || fin != f.famFinish {
					goto skipFam
				}
			}
			for _, it := range famProducts(pages, pageOrder, id) {
				if it.finish == "" && f.rowFinish[it.productID] == "" {
					if f.brandCode == "CHI" && !strings.HasSuffix(strings.ToUpper(it.sku), "CP") && f.famFinish == "Polished Chrome" {
						continue
					}
					finishUpdates = append(finishUpdates, [2]any{it.productID, f.famFinish})
				}
			}
		}
	skipFam:
		fb := map[string]bool{}
		for _, fin := range f.finishes {
			fb[fin] = true
		}
		finishByFam[id] = fb
	}
	for _, id := range ids {
		f := fams[id]
		if len(f.facts) > 0 {
			st.WithFacts++
		}
		for k := range f.facts {
			st.ByKey[k]++
			st.Facts++
		}
		if f.coll != "" {
			st.Collections++
		}
		if len(f.bullets) > 0 {
			st.Bullets++
		}
		if f.desc != "" {
			st.Descriptions++
		}
	}
	st.FinishesSet = len(finishUpdates)
	if dry {
		return st, samples, nil
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return st, samples, err
	}
	defer tx.Rollback(ctx)
	// finishes first so "Available Finishes" can include them
	for i := 0; i < len(finishUpdates); i += 500 {
		b := &pgx.Batch{}
		for _, u := range finishUpdates[i:min(len(finishUpdates), i+500)] {
			b.Queue(`UPDATE products SET finish=$2 WHERE id=$1 AND COALESCE(finish,'')=''`, u[0], u[1])
		}
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return st, samples, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM family_facts ff USING product_families f, brands b
		WHERE ff.family_id=f.id AND f.brand_id=b.id AND ff.source='catalog' AND ($1='' OR b.code=$1)`, brandCode); err != nil {
		return st, samples, err
	}
	// product finishes already on file, to union with the printed names
	prow, err := tx.Query(ctx, `SELECT p.family_id, p.finish FROM products p JOIN brands b ON b.id=p.brand_id
		WHERE p.family_id IS NOT NULL AND COALESCE(p.finish,'')<>'' AND ($1='' OR b.code=$1)`, brandCode)
	if err != nil {
		return st, samples, err
	}
	for prow.Next() {
		var fid int64
		var fin string
		if err := prow.Scan(&fid, &fin); err != nil {
			prow.Close()
			return st, samples, err
		}
		if fb := finishByFam[fid]; fb != nil {
			fb[fin] = true
		}
	}
	prow.Close()
	b := &pgx.Batch{}
	flush := func() error {
		if b.Len() == 0 {
			return nil
		}
		err := tx.SendBatch(ctx, b).Close()
		b = &pgx.Batch{}
		return err
	}
	const ins = `INSERT INTO family_facts (family_id, key, value, unit, source, catalog_id, page, conf) VALUES ($1,$2,$3,NULLIF($4,''),'catalog',NULLIF($5,0),NULLIF($6,0),$7)
		ON CONFLICT (family_id, key, source) DO UPDATE SET value=EXCLUDED.value, unit=EXCLUDED.unit, catalog_id=EXCLUDED.catalog_id, page=EXCLUDED.page, conf=EXCLUDED.conf, updated_at=now()`
	for _, id := range ids {
		f := fams[id]
		for k, fc := range f.facts {
			src := f.factSrc[k]
			b.Queue(ins, id, k, fc.Value, fc.Unit, src[0], src[1], fc.Conf)
		}
		if fb := finishByFam[id]; len(fb) > 0 {
			var names []string
			for n := range fb {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) > 24 {
				names = names[:24]
			}
			b.Queue(ins, id, "Available Finishes", strings.Join(names, ", "), "", 0, 0, 0.7)
		}
		if f.coll != "" {
			b.Queue(ins, id, "Collection", f.coll, "", 0, 0, f.collConf)
		}
		if f.desc != "" {
			b.Queue(ins, id, "Description", f.desc, "", 0, 0, 0.5)
		}
		if len(f.bullets) > 0 {
			b.Queue(ins, id, "Bullets", strings.Join(f.bullets, "\n"), "", 0, 0, 0.6)
		}
		if b.Len() >= 800 {
			if err := flush(); err != nil {
				return st, samples, err
			}
		}
	}
	if err := flush(); err != nil {
		return st, samples, err
	}
	if err := mergeFacts(ctx, tx, brandCode); err != nil {
		return st, samples, err
	}
	if err := tx.Commit(ctx); err != nil {
		return st, samples, err
	}
	_, err = s.Pool.Exec(ctx, `SELECT refresh_family_blob()`)
	return st, samples, err
}

// mergeFacts folds family_facts into product_families. The site wins: a site family keeps
// every attribute it has and only gains keys it lacks; a catalog family's attributes are
// rebuilt from its facts. Collection, description and bullets fill only when empty (or on
// catalog families, where they can only have come from an earlier run).
func mergeFacts(ctx context.Context, tx pgx.Tx, brandCode string) error {
	const meta = `('Collection','Description','Bullets')`
	for _, q := range []string{
		`UPDATE product_families f SET attributes = sub.j, updated_at = now()
		 FROM (SELECT ff.family_id, jsonb_object_agg(ff.key, ff.value ORDER BY ff.key) j FROM family_facts ff
		       WHERE ff.source='catalog' AND ff.key NOT IN ` + meta + ` GROUP BY ff.family_id) sub, brands b
		 WHERE f.id=sub.family_id AND f.brand_id=b.id AND f.source='catalog' AND ($1='' OR b.code=$1) AND f.attributes IS DISTINCT FROM sub.j`,
		`UPDATE product_families f SET attributes = f.attributes || sub.j, updated_at = now()
		 FROM (SELECT ff.family_id, jsonb_object_agg(ff.key, ff.value ORDER BY ff.key) j FROM family_facts ff JOIN product_families g ON g.id=ff.family_id
		       WHERE ff.source='catalog' AND ff.key NOT IN ` + meta + ` AND NOT (g.attributes ? ff.key) GROUP BY ff.family_id) sub, brands b
		 WHERE f.id=sub.family_id AND f.brand_id=b.id AND f.source<>'catalog' AND ($1='' OR b.code=$1)`,
		`UPDATE product_families f SET collection = ff.value, search = to_tsvector('english', concat_ws(' ', f.title, f.family_key, ff.value)), updated_at = now()
		 FROM family_facts ff, brands b WHERE ff.family_id=f.id AND f.brand_id=b.id AND ff.source='catalog' AND ff.key='Collection'
		   AND (COALESCE(f.collection,'')='' OR f.source='catalog') AND f.collection IS DISTINCT FROM ff.value AND ($1='' OR b.code=$1)`,
		`UPDATE product_families f SET description = ff.value, updated_at = now()
		 FROM family_facts ff, brands b WHERE ff.family_id=f.id AND f.brand_id=b.id AND ff.source='catalog' AND ff.key='Description'
		   AND (COALESCE(f.description,'')='' OR f.source='catalog') AND f.description IS DISTINCT FROM ff.value AND ($1='' OR b.code=$1)`,
		`UPDATE product_families f SET bullets = string_to_array(ff.value, E'\n'), updated_at = now()
		 FROM family_facts ff, brands b WHERE ff.family_id=f.id AND f.brand_id=b.id AND ff.source='catalog' AND ff.key='Bullets'
		   AND (cardinality(f.bullets)=0 OR f.source='catalog') AND f.bullets IS DISTINCT FROM string_to_array(ff.value, E'\n') AND ($1='' OR b.code=$1)`,
	} {
		if _, err := tx.Exec(ctx, q, brandCode); err != nil {
			return err
		}
	}
	return nil
}

// merge folds one page's result into the family, keeping the most confident fact per key.
func (f *infoFam) merge(r catinfo.Result, cat, page int) {
	for _, fc := range r.Facts {
		if cur, ok := f.facts[fc.Key]; !ok || fc.Conf > cur.Conf {
			f.facts[fc.Key] = fc
			f.factSrc[fc.Key] = [2]int{cat, page}
		}
	}
	if r.Collection != "" && r.CollConf > f.collConf {
		f.coll, f.collConf = r.Collection, r.CollConf
	}
	if len(r.Description) > len(f.desc) {
		f.desc = r.Description
	}
	seen := map[string]bool{}
	for _, b := range f.bullets {
		seen[strings.ToLower(b)] = true
	}
	for _, b := range r.Bullets {
		if !seen[strings.ToLower(b)] && len(f.bullets) < 12 {
			seen[strings.ToLower(b)] = true
			f.bullets = append(f.bullets, b)
		}
	}
	for _, fin := range r.Finishes {
		dup := false
		for _, x := range f.finishes {
			if x == fin {
				dup = true
			}
		}
		if !dup {
			f.finishes = append(f.finishes, fin)
		}
	}
	// a block that names exactly one finish ("Polished chrome plated finish") finishes every SKU
	if len(r.Finishes) == 1 && (f.famFinish == "" || f.famFinish == r.Finishes[0]) {
		f.famFinish = r.Finishes[0]
	} else if len(r.Finishes) > 1 {
		f.famFinish = "?"
	}
	if f.famFinish == "?" {
		f.famFinish = ""
	}
}

type pageKey struct{ cat, page int }

type pageData struct {
	cat, page int
	bcode     string
	bname     string
	cname     string
	items     []infoItem
}

func famProducts(pages map[pageKey]*pageData, order []pageKey, fid int64) []infoItem {
	var out []infoItem
	seen := map[int64]bool{}
	for _, k := range order {
		for _, it := range pages[k].items {
			if it.familyID == fid && it.productID != 0 && !seen[it.productID] {
				seen[it.productID] = true
				out = append(out, it)
			}
		}
	}
	return out
}

// readingOrder sorts lines by row (Y within a line height) then X.
func readingOrder(lines []pageLine) []pageLine {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		tol := max(8, min(a.H, b.H)/2)
		if abs(a.Y-b.Y) > tol {
			return a.Y < b.Y
		}
		return a.X0 < b.X0
	})
	return lines
}

// findItemLine returns the index of the line holding the item's SKU, or -1.
func findItemLine(lines []pageLine, it infoItem) int {
	best, bestD := -1, 1<<30
	u := strings.ToUpper(it.sku)
	for i, l := range lines {
		if it.y < l.Y-12 || it.y > l.Y+max(l.H, 20)+12 || it.x < l.X0-40 || it.x > l.X1+40 {
			continue
		}
		d := abs(l.Y - it.y)
		if !strings.Contains(strings.ToUpper(l.Text), u) {
			d += 100
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// assignRuns clusters SKU rows that follow each other at table spacing.
func assignRuns(items []infoItem, lines []pageLine, itemLine []int) []int {
	runOf := make([]int, len(items))
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return items[idx[a]].y < items[idx[b]].y })
	run, lastY, lastH := 0, -1<<30, 20
	for _, i := range idx {
		it := items[i]
		h := lastH
		if itemLine[i] >= 0 && lines[itemLine[i]].H > 0 {
			h = lines[itemLine[i]].H
		}
		if it.y-lastY > max(60, 5*h/2) {
			run++
		}
		runOf[i] = run
		lastY, lastH = it.y, h
	}
	return runOf
}

// rowFinishFor names the finish printed on an item's own row: either a finish name
// ("TS-30-30 Wall Tub Spout polished chrome 335") or a finish-code token that the SKU also
// carries ("WH | 3-3064WH 4" centerset", "ORB [LFW100-BC-ORB …").
func rowFinishFor(it infoItem, lines []pageLine, li int, brandCodes, generic map[string]string) string {
	line := it.line
	if li >= 0 {
		line = lines[li].Text
	}
	if fin := catinfo.LineFinish(line); fin != "" {
		return fin
	}
	if fin := catinfo.LineFinish(it.line); fin != "" {
		return fin
	}
	codes := brandCodes
	if len(codes) == 0 {
		codes = generic
	}
	u := strings.ToUpper(it.sku)
	got := ""
	for _, w := range strings.Fields(strings.ToUpper(line)) {
		w = strings.Trim(w, "|[](),.;:")
		if len(w) < 2 || len(w) > 4 || w == u {
			continue
		}
		fin, ok := codes[w]
		if !ok || !strings.Contains(u, w) {
			continue
		}
		if got != "" && got != fin {
			return ""
		}
		got = fin
	}
	return got
}

type tocIndex struct {
	byCat map[int][]tocEntry
}

type tocEntry struct {
	page, level int
	title       string
	printed     bool
}

// entries returns the printed contents entries covering a page: the latest level-2 and
// level-1 entries at or before it. OCR'd contents entries are page headings, which
// pageHeading already supplies with better junk control, so they are not used here.
func (t tocIndex) entries(cat, page int) []string {
	var l2, l1 string
	for _, e := range t.byCat[cat] {
		if e.page > page {
			break
		}
		switch {
		case e.printed && e.level >= 2:
			l2 = e.title
		case e.printed:
			l1 = e.title
		}
	}
	var out []string
	for _, s := range []string{l2, l1} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (s *Store) loadTOC(ctx context.Context, brandCode string) (tocIndex, error) {
	t := tocIndex{byCat: map[int][]tocEntry{}}
	rows, err := s.Pool.Query(ctx, `SELECT t.catalog_id, t.page, t.level, t.title, t.source='printed' FROM catalog_toc t JOIN catalogs c ON c.id=t.catalog_id
		WHERE ($1='' OR c.brand_code=$1) ORDER BY t.catalog_id, t.page, t.level`, brandCode)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var e tocEntry
		var cat int
		if err := rows.Scan(&cat, &e.page, &e.level, &e.title, &e.printed); err != nil {
			return t, err
		}
		t.byCat[cat] = append(t.byCat[cat], e)
	}
	return t, rows.Err()
}

// loadFinishCodes returns brand → code → finish, with the generic map under "*".
func (s *Store) loadFinishCodes(ctx context.Context) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	rows, err := s.Pool.Query(ctx, `SELECT brand_code, upper(code), finish FROM finish_codes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b, c, f string
		if err := rows.Scan(&b, &c, &f); err != nil {
			return nil, err
		}
		if out[b] == nil {
			out[b] = map[string]string{}
		}
		out[b][c] = f
	}
	return out, rows.Err()
}
