package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"georges_/internal/ocr"
)

// Layout-aware retitling. Price books rarely put the product name on the SKU's own line: it
// is a bold group header above a finish table (Barclay), the description row at the top of a
// block (California Faucets), or a full-width product line above a size grid (Duravit). This
// pass looks at the OCR word boxes around each SKU and picks the best prose line above it.

type pageLine struct {
	Y, X0, X1, H int
	Text         string
}

func loadPageLines(path string) ([]pageLine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var words []ocr.Word
	if err := json.Unmarshal(b, &words); err != nil {
		return nil, err
	}
	type key struct{ b, p, l int }
	groups := map[key][]ocr.Word{}
	var order []key
	for _, w := range words {
		k := key{w.Block, w.Par, w.Line}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], w)
	}
	var lines []pageLine
	for _, k := range order {
		ws := groups[k]
		sort.Slice(ws, func(i, j int) bool { return ws[i].X < ws[j].X })
		// tesseract often joins side-by-side columns into one line; split at wide gaps
		var seg []ocr.Word
		flush := func() {
			if len(seg) == 0 {
				return
			}
			l := pageLine{Y: seg[0].Y, X0: seg[0].X, X1: seg[0].X + seg[0].W}
			var parts []string
			for _, w := range seg {
				if w.Y < l.Y {
					l.Y = w.Y
				}
				if w.X < l.X0 {
					l.X0 = w.X
				}
				if w.X+w.W > l.X1 {
					l.X1 = w.X + w.W
				}
				if w.H > l.H {
					l.H = w.H
				}
				parts = append(parts, w.Text)
			}
			l.Text = strings.Join(parts, " ")
			lines = append(lines, l)
			seg = nil
		}
		for i, w := range ws {
			if i > 0 {
				prev := ws[i-1]
				if gap := w.X - (prev.X + prev.W); gap > max(110, 4*prev.H) {
					flush()
				}
			}
			seg = append(seg, w)
		}
		flush()
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Y < lines[j].Y })
	return lines, nil
}

var headerWords = map[string]bool{
	"description": true, "price": true, "prices": true, "finish": true, "finishes": true, "model": true, "item": true,
	"color": true, "colour": true, "number": true, "pricing": true, "part": true, "qty": true, "sku": true, "list": true,
	"stock": true, "type": true, "code": true, "size": true, "dimensions": true, "gpf": true, "gpm": true, "no.": true,
}

// sentence/marketing/caption words: a product name never contains these
var proseWords = map[string]bool{
	"ideal": true, "delivers": true, "reduces": true, "ensures": true, "provides": true, "offers": true, "test": true,
	"result": true, "results": true, "confirmed": true, "page": true, "pages": true, "view": true, "shown": true,
	"required": true, "upgrade": true, "only": true, "must": true, "additional": true, "please": true, "prefer": true,
	"included": true, "includes": true, "available": true, "standard": true, "warranty": true, "call": true,
	"contact": true, "visit": true, "www": true, "com": true, "against": true, "organisms": true, "meet": true,
	"exceed": true, "designed": true, "engineered": true, "quality": true, "sustainability": true, "each": true,
	"per": true, "sold": true, "separately": true, "starting": true, "lead": true, "time": true, "charge": true,
	"net": true, "crate": true, "export": true, "you": true, "your": true, "our": true, "we": true, "is": true, "are": true,
	"featured": true, "patented": true, "created": true, "add": true, "adds": true, "simplified": true, "everything": true,
	"source": true, "kind": true, "every": true, "some": true, "its": true, "it's": true, "connection": true,
	"installation": true, "maintenance": true, "matching": true, "match": true, "headquarters": true, "new": true,
	"recently": true, "released": true, "index": true, "key": true, "what's": true, "none": true, "remote": true,
	"control": true, "colors": true, "colours": true, "option": true, "options": true, "or": true, "than": true,
	"more": true, "most": true, "any": true, "such": true, "this": true, "that": true, "these": true, "those": true,
	"pictured": true, "picture": true, "right": true, "left": true, "above": true, "below": true, "refer": true, "listed": true,
}

var noteStarts = map[string]bool{
	"order": true, "see": true, "note": true, "notes": true, "includes": true, "include": true, "included": true,
	"available": true, "optional": true, "requires": true, "required": true, "for": true, "with": true, "all": true,
	"also": true, "please": true, "ships": true, "must": true, "add": true, "use": true, "not": true, "sold": true,
	"shown": true, "fits": true, "and": true, "or": true, "the": true, "to": true, "when": true, "if": true,
	"specify": true, "select": true, "choose": true, "comes": true, "supplied": true, "recommended": true,
}

// usableLine rejects table headers, footnotes, spec bullets and SKU/finish rows.
func usableLine(t string) bool {
	s := strings.TrimSpace(t)
	if s == "" || strings.ContainsAny(s[:1], "*+«•-–—·>›¢§") {
		return false
	}
	f := strings.Fields(s)
	if len(f) > 10 || len(s) > 80 {
		return false // a paragraph, not a name
	}
	if r := s[0]; r >= 'a' && r <= 'z' {
		return false // continuation of a sentence
	}
	if strings.Contains(s, ". ") || strings.Contains(s, "; ") || strings.Contains(s, ":") {
		return false // mid-sentence or a "label: value" spec line
	}
	if strings.EqualFold(s, "table of contents") || strings.EqualFold(s, "contents") {
		return false
	}
	if noteStarts[strings.ToLower(strings.Trim(f[0], ",.:;"))] {
		return false
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, ",") || strings.HasSuffix(s, ":") {
		return false
	}
	last := strings.ToLower(f[len(f)-1])
	if last == "and" || last == "or" || last == "with" || last == "for" || last == "the" || last == "in" || last == "to" || last == "of" || last == "a" {
		return false // cut-off sentence
	}
	headers := 0
	for _, w := range f {
		lw := strings.ToLower(strings.Trim(w, ",.:;()"))
		if headerWords[lw] {
			headers++
		}
		if proseWords[lw] {
			return false
		}
		if mixedSKUToken(w) {
			return false
		}
	}
	if headers >= 2 || (headers == 1 && len(f) <= 3) {
		return false // "Item Number List Price", "Model No."
	}
	if headers == 1 && len(f) >= 4 {
		allCap := true
		for _, w := range f {
			if r := w[0]; r < 'A' || r > 'Z' {
				allCap = false
			}
		}
		if allCap {
			return false // "Cartridge Handle Outlet Spout Model": a spec-table header row
		}
	}
	return true
}

// unrepeat collapses a phrase repeated 2–4 times ("C. Sink Faucet C. Sink Faucet C. Sink Faucet").
func unrepeat(t string) string {
	w := strings.Fields(t)
	for reps := 4; reps >= 2; reps-- {
		if n := len(w); n >= reps*2 && n%reps == 0 {
			h := n / reps
			ok := true
			for r := 1; r < reps && ok; r++ {
				ok = strings.EqualFold(strings.Join(w[:h], " "), strings.Join(w[r*h:(r+1)*h], " "))
			}
			if ok {
				return strings.Join(w[:h], " ")
			}
		}
	}
	return t
}

// mixedSKUToken: letters and ≥2 digits in SKU shape (55720-BN, LC0009, 3001F-1); a plain
// number (a price or dimension) is not enough on its own.
func mixedSKUToken(w string) bool {
	u := strings.ToUpper(strings.Trim(w, ",;|."))
	if !reSKUish.MatchString(u) {
		return false
	}
	letters, digits := 0, 0
	for _, r := range u {
		switch {
		case r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	return letters >= 1 && digits >= 2
}

// layoutTitle finds the best product line above the SKU at (skuX, skuY).
func layoutTitle(lines []pageLine, skuX, skuY int, brand string) (string, int) {
	// the SKU's own line gives the column extent and the body font size
	sx0, sx1, sh := skuX-50, skuX+600, 20
	for _, l := range lines {
		if abs(l.Y-skuY) <= 8 && l.X0-20 <= skuX && skuX <= l.X1+20 {
			sx0, sx1, sh = l.X0, l.X1, l.H
			break
		}
	}
	best, bestScore := "", -1
	for _, l := range lines {
		d := skuY - l.Y
		if d <= 8 || d > 800 {
			continue
		}
		if max(l.X0, sx0) >= min(l.X1, sx1) {
			continue // another column
		}
		if l.X1-l.X0 < l.H { // rotated running head
			continue
		}
		if !usableLine(l.Text) {
			continue
		}
		if brand != "" && strings.Contains(strings.ToLower(l.Text), strings.ToLower(brand)) {
			continue // "Duravit attaches great importance to…"
		}
		t := trimTitle(unrepeat(cleanTitle(l.Text, "", 0)))
		sc := scoreTitle(t)
		if sc < 10 {
			continue
		}
		header := sh > 0 && l.H*4 >= sh*5 // larger/bolder than the table body
		if len(strings.Fields(t)) <= 2 && !header {
			continue // "Vitreous china": a material note, not a name
		}
		sc -= d / 60
		if header {
			sc += 8
		}
		if sc >= 15 && sc > bestScore {
			best, bestScore = t, sc
		}
	}
	return best, bestScore
}

// pageHeading returns the page's display heading or "": first the widest, tallest line in the
// top band of the page (a price-guide running head, "M Series Reserve Medicine Cabinets"), else
// the tallest wide line anywhere. OCR line heights are unreliable lower down — a product drawing
// merged into a table row makes "Polished Edge" the tallest line on a Robern page — so the top
// band is trusted before height is.
func pageHeading(lines []pageLine, brand string) string {
	const topBand = 260 // of a 2200 px page render
	best, bestH := "", 0
	for _, l := range lines {
		if l.Y > topBand || l.H < 20 || l.X1-l.X0 < 300 {
			continue
		}
		if t, ok := headingText(strings.ReplaceAll(l.Text, ".", " "), brand); ok && l.H > bestH {
			best, bestH = t, l.H
		}
	}
	if best != "" {
		return best
	}
	maxH := 0
	for _, l := range lines {
		if l.X1-l.X0 >= l.H && l.H > maxH {
			maxH = l.H
		}
	}
	if maxH < 36 {
		return ""
	}
	for _, l := range lines {
		if l.H*10 < maxH*9 || l.X1-l.X0 < l.H {
			continue
		}
		if t, ok := headingText(l.Text, brand); ok {
			return t
		}
	}
	return ""
}

// headingText cleans an OCR line and reports whether it reads like a collection heading: up to
// six real words, no digits or sentence punctuation, not the maker's own name.
func headingText(line, brand string) (string, bool) {
	t := cleanTitle(reTMNoise.ReplaceAllString(line, "$1"), "", 0)
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 6 || !goodHeading(t) || strings.ContainsAny(t, ".:;\"'/-0123456789") {
		return "", false
	}
	if r := t[0]; r >= 'a' && r <= 'z' {
		return "", false
	}
	if len(words) == 1 && len(words[0]) < 5 {
		return "", false
	}
	real := 0
	for _, w := range words {
		w = strings.Trim(w, ",;:™®")
		if len(w) <= 2 {
			continue // "M" in "M Series Reserve Medicine Cabinets"
		}
		if wordClass(w) != "real" || len(w) < 4 || headerWords[strings.ToLower(w)] || proseWords[strings.ToLower(w)] {
			return "", false
		}
		real++
	}
	if real <= 0 {
		return "", false
	}
	lb, lt := strings.ToLower(brand), strings.ToLower(t)
	if lb != "" && (strings.Contains(lt, lb) || strings.Contains(lb, lt)) {
		return "", false // the maker's logo, not a collection name
	}
	for _, w := range words {
		if bookPageWords[strings.ToLower(strings.Trim(w, ",;:"))] {
			return "", false // "Ganging Reference", "Vanities Worksheet": a page about the line, not the line
		}
	}
	if strings.ToUpper(t) == t {
		t = titleCaseWords(t)
	}
	return t, true
}

// bookPageWords mark running heads of reference pages, which never name a collection.
var bookPageWords = map[string]bool{
	"reference": true, "worksheet": true, "guide": true, "index": true, "contents": true, "ganging": true,
	"specifications": true, "dimensions": true, "program": true, "warranty": true, "terms": true, "policy": true,
	"installation": true, "overview": true, "notes": true, "chart": true, "matrix": true, "options": true,
}

// reTMNoise is a trademark sign the OCR read as a trailing letter: "RESERVEw", "TENORw~".
var reTMNoise = regexp.MustCompile(`([A-Z]{3,})(w|tm|TM)[~']*\b`)

// containsFold reports whether a contains b, ignoring case.
func containsFold(a, b string) bool {
	return strings.Contains(strings.ToLower(a), strings.ToLower(b))
}

var reJunkTitle = regexp.MustCompile(`[:_]|\d{5,}|\.$|\d[a-z]{2,}`)

// badTitle: the current title is a key, a contents heading, OCR debris, or a generic label
// shared by many families of the brand ("Kitchen and Farmer Sinks" ×119).
func badTitle(title, key string, isTOC bool, shared int) bool {
	t := strings.TrimSpace(title)
	if t == "" || t == key || isTOC || shared >= 4 || scoreTitle(t) < 0 || reJunkTitle.MatchString(t) || len(t) > 70 {
		return true
	}
	if w := strings.Fields(strings.ToLower(t)); len(w) > 0 && noteStarts[w[0]] {
		return true
	}
	for _, w := range strings.Fields(strings.ToLower(t)) {
		if proseWords[strings.Trim(w, ",.:;()")] {
			return true // "CP Tub fillers are pictured to the right" is a caption, not a name
		}
	}
	return false
}

// RetitleChange records one proposed/applied title change.
type RetitleChange struct {
	ID       int64
	Brand    string
	Old, New string
}

// RetitleFromLayout retitles catalog families whose title is junk using the page layout around
// their SKUs. dataDir holds catalogs/<BRAND>/<book>/NNNN.words.json. With dry, nothing is saved.
func (s *Store) RetitleFromLayout(ctx context.Context, dataDir, brandCode string, dry bool) ([]RetitleChange, error) {
	toc := map[int]map[string]bool{}
	trows, err := s.Pool.Query(ctx, `SELECT catalog_id, lower(title) FROM catalog_toc`)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var c int
		var t string
		if err := trows.Scan(&c, &t); err != nil {
			trows.Close()
			return nil, err
		}
		if toc[c] == nil {
			toc[c] = map[string]bool{}
		}
		toc[c][strings.TrimSpace(t)] = true
	}
	trows.Close()

	rows, err := s.Pool.Query(ctx, `
		SELECT f.id, COALESCE(f.title,''), f.family_key, b.id, b.name, c.id, c.brand_code, c.name, i.page, i.x, i.y
		FROM product_families f
		JOIN brands b ON b.id=f.brand_id
		JOIN products p ON p.family_id=f.id
		JOIN catalogs c ON c.brand_code=b.code
		JOIN catalog_items i ON i.catalog_id=c.id AND i.sku=p.sku
		WHERE f.source='catalog' AND ($1='' OR b.code=$1)
		ORDER BY c.id, i.page, f.id`, brandCode)
	if err != nil {
		return nil, err
	}
	type item struct{ cat, page, x, y int }
	type fam struct {
		id         int64
		title, key string
		brandID    int
		brand      string
		isTOC      bool
		items      []item
		catDir     map[int]string
	}
	fams := map[int64]*fam{}
	var order []int64
	shared := map[string]int{}
	for rows.Next() {
		var id int64
		var title, key, brand, bcode, cname string
		var bid, cid, page, x, y int
		if err := rows.Scan(&id, &title, &key, &bid, &brand, &cid, &bcode, &cname, &page, &x, &y); err != nil {
			rows.Close()
			return nil, err
		}
		f := fams[id]
		if f == nil {
			f = &fam{id: id, title: title, key: key, brandID: bid, brand: brand, catDir: map[int]string{}}
			fams[id] = f
			order = append(order, id)
			shared[fmt.Sprintf("%d|%s", bid, strings.ToLower(strings.TrimSpace(title)))]++
		}
		if toc[cid][strings.ToLower(strings.TrimSpace(title))] {
			f.isTOC = true
		}
		if len(f.items) < 8 {
			f.items = append(f.items, item{cid, page, x, y})
			f.catDir[cid] = filepath.Join(dataDir, "catalogs", bcode, safeDirName(cname))
		}
	}
	rows.Close()

	cache := map[string][]pageLine{}
	var changes []RetitleChange
	for _, id := range order {
		f := fams[id]
		sharedN := shared[fmt.Sprintf("%d|%s", f.brandID, strings.ToLower(strings.TrimSpace(f.title)))]
		if !badTitle(f.title, f.key, f.isTOC, sharedN) {
			continue
		}
		best, bestScore, heading := "", -1, ""
		for _, it := range f.items {
			p := filepath.Join(f.catDir[it.cat], fmt.Sprintf("%04d.words.json", it.page))
			lines, ok := cache[p]
			if !ok {
				lines, _ = loadPageLines(p)
				if len(cache) > 400 {
					cache = map[string][]pageLine{}
				}
				cache[p] = lines
			}
			if len(lines) == 0 {
				continue
			}
			t, sc := layoutTitle(lines, it.x, it.y, f.brand)
			if sc > bestScore {
				best, bestScore = t, sc
				heading = pageHeading(lines, f.brand)
			} else if heading == "" {
				heading = pageHeading(lines, f.brand)
			}
		}
		title := best
		shortGeneric := !f.isTOC && f.title != f.key && scoreTitle(f.title) >= 0 && len(strings.Fields(f.title)) <= 4 && !reJunkTitle.MatchString(f.title)
		if heading != "" && shortGeneric && bestScore < 30 {
			title = f.title // "Combo System" under the AIMEE heading beats a weak line above the SKU
		}
		descriptive := !f.isTOC && f.title != f.key && scoreTitle(f.title) >= 0 && len(strings.Fields(f.title)) <= 8 && !reJunkTitle.MatchString(f.title)
		// the line above is another model's description, not a group: shared as a title, or short /
		// comma-listed like the row descriptions ("Polished Edge", "Bevel Edge, Reversible Hinge")
		siblingRow := shared[fmt.Sprintf("%d|%s", f.brandID, strings.ToLower(strings.TrimSpace(best)))] >= 2 ||
			len(strings.Fields(best)) < 3 || strings.Contains(best, ",")
		if descriptive && bestScore >= 30 && best != "" && sharedN >= 4 && !siblingRow && !containsFold(best, f.title) && !containsFold(f.title, best) {
			// the current title is a row description many models share ("Bevel Edge, Right Hinge")
			// and the confident line above the SKU is the group it belongs to ("M Series Reserve
			// Medicine Cabinets"): keep both, the group first
			title = best + " · " + f.title
		}
		if title == "" {
			continue
		}
		if heading != "" && len(strings.Fields(title)) <= 5 && !strings.EqualFold(heading, f.brand) &&
			!strings.Contains(strings.ToLower(title), strings.ToLower(heading)) {
			title = heading + " · " + title
		}
		if title == f.title {
			continue
		}
		changes = append(changes, RetitleChange{ID: f.id, Brand: f.brand, Old: f.title, New: title})
		if dry {
			continue
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE product_families SET title=$2, search=to_tsvector('english',$2||' '||family_key) WHERE id=$1`, f.id, title); err != nil {
			return changes, err
		}
	}
	if !dry && len(changes) > 0 {
		if _, err := s.Pool.Exec(ctx, `SELECT refresh_family_blob()`); err != nil {
			return changes, err
		}
	}
	return changes, nil
}

// safeDirName mirrors the catalog download directory naming (cmd/georges safeName).
func safeDirName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
