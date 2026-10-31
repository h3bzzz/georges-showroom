package store

import (
	"context"
	"regexp"
	"strings"
)

type ItemRow struct {
	CatalogID  int
	Page       int
	SKU        string
	Line       string
	PriceCents int64
	X, Y       int
}

func (s *Store) SaveItems(ctx context.Context, catalogID, page int, items []ItemRow) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_items WHERE catalog_id=$1 AND page=$2`, catalogID, page); err != nil {
		return err
	}
	for _, it := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_items (catalog_id, page, sku, sku_base, line, price_cents, x, y) VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,0),$7,$8) ON CONFLICT DO NOTHING`,
			catalogID, page, it.SKU, SKUBase(it.SKU), it.Line, it.PriceCents, it.X, it.Y); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

var reNoise = regexp.MustCompile(`\s{2,}`)

// cleanTitle removes the SKU, price, spec-table debris (weights, widths, OCR symbols) and
// doubled phrases from an OCR line to leave a product description.
func cleanTitle(line, sku string, price int64) string {
	t := strings.ReplaceAll(reTMNoise.ReplaceAllString(line, "$1"), sku, "")
	t = strings.NewReplacer("$", "", "*", "", "†", "", "•", "", "¢", "", "©", "", "®", "", "™", "", "|", " ", "«", " ", "»", " ", " + ", " ", " @", " ").Replace(t)
	t = strings.TrimLeft(t, " +:;-–—·.")
	words := strings.Fields(t)
	var keep []string
	for i, w := range words {
		tw := strings.Trim(w, ",;:")
		if tw == "" || rePrice.MatchString(tw) || looksLikeSKUToken(w) || reMeasure.MatchString(tw) || specWords[strings.ToLower(tw)] {
			continue
		}
		if len(w) == 1 && !strings.ContainsAny(w, "&/") && !(w[0] >= 'A' && w[0] <= 'Z' && i+1 < len(words) && seriesWord(words[i+1])) {
			continue // a lone letter is noise unless it names a line: "M Series", "R3 Series"
		}
		if !asciiWord(tw) {
			continue // "§7495", "1%”", "——", "H_"
		}
		if len(keep) == 0 && !hasLetter(tw) {
			continue // leading numbers and fragments: `length 2" Double Robe Hook`
		}
		keep = append(keep, w)
	}
	out := reNoise.ReplaceAllString(strings.TrimSpace(strings.Join(keep, " ")), " ")
	out = strings.TrimRight(out, " ,;:-–—")
	out = undouble(out)
	if out != "" && strings.ToUpper(out) == out {
		out = titleCaseWords(out)
	}
	return out
}

var reMeasure = regexp.MustCompile(`(?i)^(t|\d+(\.\d+)?)(lb|lbs|oz|mm|cm|kg|in|ft|g)\.?$`)

var specWords = map[string]bool{
	"width": true, "length": true, "height": true, "depth": true, "wide": true, "long": true,
	"dia": true, "diameter": true, "oz": true, "lb": true, "lbs": true, "toz": true, "tlb": true,
	"weight": true, "wt": true, "ctc": true, "c/c": true, "proj": true, "projection": true,
	"gpm": true, "psi": true, "kpa": true, "lpm": true, "l/min": true,
	"model": true, "no.": true, "no": true, "item": true, "art.": true, "ref.": true, "code": true,
}

// asciiWord: letters, digits and light punctuation only; anything else is OCR noise.
func asciiWord(w string) bool {
	for _, r := range w {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case strings.ContainsRune("-'\"/&(),.:;%", r):
		default:
			return false
		}
	}
	return true
}

// undouble collapses "Robe Hook Robe Hook" into "Robe Hook".
func undouble(t string) string {
	w := strings.Fields(t)
	if n := len(w); n >= 2 && n%2 == 0 {
		h := n / 2
		if strings.EqualFold(strings.Join(w[:h], " "), strings.Join(w[h:], " ")) {
			return strings.Join(w[:h], " ")
		}
	}
	return t
}

func titleCaseWords(t string) string {
	w := strings.Fields(strings.ToLower(t))
	for i, x := range w {
		if len(x) > 0 && (i == 0 || len(x) > 2) {
			w[i] = strings.ToUpper(x[:1]) + x[1:]
		}
	}
	return strings.Join(w, " ")
}

var rePrice = regexp.MustCompile(`^\$?(\d{1,3}(?:,\d{3})+|\d{2,6})(?:\.\d{2})?$`)

var reSKUish = regexp.MustCompile(`^[A-Z0-9][A-Z0-9./|-]{2,}$`)

func looksLikeSKUToken(w string) bool {
	u := strings.ToUpper(strings.Trim(w, ",;|"))
	if !reSKUish.MatchString(u) {
		return false
	}
	d := 0
	for _, r := range u {
		if r >= '0' && r <= '9' {
			d++
		}
	}
	return d >= 2
}

// sectionTitle returns the contents entry covering a page ("Descanso · K30 Series").
func (s *Store) sectionTitle(ctx context.Context, catalogID, page int) string {
	var t string
	_ = s.Pool.QueryRow(ctx, `
		SELECT title FROM catalog_toc WHERE catalog_id=$1 AND page<=$2
		ORDER BY (source='printed') DESC, page DESC, level DESC LIMIT 1`, catalogID, page).Scan(&t)
	return t
}

// hasRealWord: a usable title has at least two plain words (3+ letters, not SKU-like), or
// one long dictionary-looking word followed by nothing suspicious. OCR junk like
// "Attomiiodaras" or "¢ Ceramlceattridge" fails and gets the section title instead.
func hasRealWord(t string) bool {
	words := 0
	for _, w := range strings.Fields(t) {
		letters, other := 0, 0
		for _, r := range w {
			switch {
			case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
				letters++
			case r == '-' || r == '\'' || r == '/':
			default:
				other++
			}
		}
		if other > 0 || letters < 3 || looksLikeSKUToken(w) || hasTripleLetters(w) {
			continue
		}
		words++
	}
	return words >= 2
}

func hasTripleLetters(w string) bool {
	lw := strings.ToLower(w)
	for i := 2; i < len(lw); i++ {
		if lw[i] == lw[i-1] && lw[i] == lw[i-2] {
			return true
		}
	}
	return false
}

// ProductsFromCatalogs creates families/products for SKUs that appear in price books but
// not yet in the products table. Titles come from the OCR line; prices from the line's
// right-most money token. Returns created SKU count.
func (s *Store) ProductsFromCatalogs(ctx context.Context, brandCode string) (int, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (c.brand_code, i.sku) c.brand_code, b.id, b.name, COALESCE(c.title,c.name), i.catalog_id, i.page, i.sku, COALESCE(i.sku_base,i.sku), i.line, COALESCE(i.price_cents,0)
		FROM catalog_items i JOIN catalogs c ON c.id=i.catalog_id JOIN brands b ON b.code=c.brand_code
		WHERE ($1='' OR c.brand_code=$1)
		  AND NOT EXISTS (SELECT 1 FROM products p WHERE p.brand_id=b.id AND p.sku=i.sku)
		  AND length(i.sku) >= 4 AND length(i.line) >= 8
		ORDER BY c.brand_code, i.sku, (i.price_cents IS NULL), c.created_raw DESC NULLS LAST, i.page`, brandCode)
	if err != nil {
		return 0, err
	}
	type it struct {
		code, brand, catTitle    string
		brandID, catalogID, page int
		sku, base, line          string
		price                    int64
	}
	var items []it
	for rows.Next() {
		var x it
		if err := rows.Scan(&x.code, &x.brandID, &x.brand, &x.catTitle, &x.catalogID, &x.page, &x.sku, &x.base, &x.line, &x.price); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, x)
	}
	rows.Close()
	sib := map[string]map[string]string{}
	n := 0
	for _, x := range items {
		if sib[x.code] == nil {
			m, err := s.brandSiblingKeys(ctx, x.code)
			if err != nil {
				return n, err
			}
			sib[x.code] = m
		}
		if k, ok := sib[x.code][strings.ToUpper(x.sku)]; ok {
			x.base = k
		}
		title := cleanTitle(x.line, x.sku, x.price)
		if !hasRealWord(title) {
			if sec := s.sectionTitle(ctx, x.catalogID, x.page); sec != "" {
				title = sec
			} else {
				title = x.sku
			}
		}
		room := RoomFor(nil, "", title+" "+x.catTitle, x.brand)
		var familyID int64
		if err := s.Pool.QueryRow(ctx, `
			INSERT INTO product_families (brand_id, family_key, title, source, room, search)
			VALUES ($1,$2,$3,'catalog',NULLIF($4,''), to_tsvector('english', $3||' '||$2))
			ON CONFLICT (brand_id, family_key) DO UPDATE SET
			  title = CASE WHEN product_families.source='catalog' AND length(EXCLUDED.title) > length(product_families.title) THEN EXCLUDED.title ELSE product_families.title END,
			  room = COALESCE(product_families.room, EXCLUDED.room)
			RETURNING id`, x.brandID, x.base, title, room).Scan(&familyID); err != nil {
			return n, err
		}
		if _, err := s.Pool.Exec(ctx, `
			INSERT INTO products (brand_id, family_id, sku, name, price_cents, source, catalog_id, catalog_page)
			VALUES ($1,$2,$3,$4,NULLIF($5,0),'catalog',$6,$7)
			ON CONFLICT (brand_id, sku) DO NOTHING`, x.brandID, familyID, x.sku, title, x.price, x.catalogID, x.page); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// wordClass sorts an OCR token into real (a word), num (a number or dimension) or junk.
func wordClass(w string) string {
	letters, digits, other := 0, 0, 0
	for _, r := range w {
		switch {
		case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune("-'/&.,\"″'()", r):
		default:
			other++
		}
	}
	switch {
	case other > 0:
		return "junk"
	case letters >= 3 && digits == 0 && !hasTripleLetters(w):
		return "real"
	case letters == 0 && digits > 0:
		return "num"
	case letters == 2 && digits == 0 && (strings.EqualFold(w, "in") || strings.EqualFold(w, "of") || strings.EqualFold(w, "w/")):
		return "real"
	}
	return "junk"
}

// scoreTitle rates a cleaned description; < 0 means unusable.
func scoreTitle(t string) int {
	real, num, junk := 0, 0, 0
	for _, w := range strings.Fields(t) {
		switch wordClass(strings.Trim(w, ",;:")) {
		case "real":
			real++
		case "num":
			num++
		default:
			junk++
		}
	}
	if real < 2 || junk > real {
		return -1
	}
	finish := 0
	for _, w := range strings.Fields(strings.ToLower(t)) {
		if finishWords[strings.Trim(w, ",;:")] {
			finish++
		}
	}
	if finish*2 >= real {
		return -1 // "Matte British Gold PVD" is a finish row, not a product name
	}
	sc := real*10 - junk*8 - num*2 - finish*6
	if endsDangling(t) || reNoteLine.MatchString(t) {
		return -1 // a cut sentence ("…bring ease and intention to") or a footnote ("for use with risers") is never a name
	}
	if n := len(t); n > 90 {
		sc -= (n - 90) / 2
	} else {
		sc += n / 4
	}
	return sc
}

// endsDangling: the last word is a preposition, article or conjunction, i.e. the text is a
// truncated sentence, not a product name.
func endsDangling(t string) bool {
	ws := strings.Fields(strings.ToLower(strings.TrimRight(t, " .,;:-")))
	return len(ws) > 0 && danglingEnd[ws[len(ws)-1]]
}

var reNoteLine = regexp.MustCompile(`(?i)\b(for use with|compatible with|sold separately|pictured|see page|not included|must be ordered|available in|refer to)\b`)

var danglingEnd = map[string]bool{"to": true, "and": true, "with": true, "for": true, "of": true, "the": true, "a": true, "an": true, "in": true, "or": true, "your": true, "our": true, "that": true, "is": true, "are": true, "by": true, "on": true, "from": true}

var finishWords = map[string]bool{
	"chrome": true, "nickel": true, "brass": true, "bronze": true, "gold": true, "black": true, "white": true,
	"matte": true, "matt": true, "polished": true, "brushed": true, "satin": true, "stainless": true, "steel": true,
	"pvd": true, "copper": true, "gunmetal": true, "gun": true, "metal": true, "graphite": true, "platinum": true,
	"titanium": true, "oil": true, "rubbed": true, "antique": true, "pewter": true, "unlacquered": true,
	"lacquered": true, "british": true, "champagne": true, "rose": true, "venetian": true, "finish": true,
	"luxe": true, "cocoa": true, "bright": true, "dark": true, "light": true, "warm": true,
}

// trimTitle cuts an over-long description at a word boundary.
func trimTitle(t string) string {
	if len(t) <= 96 {
		return t
	}
	cut := strings.LastIndex(t[:96], " ")
	if cut < 40 {
		cut = 96
	}
	return strings.TrimRight(t[:cut], " ,;:-")
}

var badHeadings = map[string]bool{
	"model": true, "description": true, "item": true, "price": true, "group": true, "index": true,
	"notes": true, "contents": true, "table of contents": true, "warranty": true, "finishes": true,
	"list price": true, "article numbers index": true, "page": true, "bath": true, "kitchen": true,
	"specifications": true, "terms": true, "introduction": true,
}

// goodHeading says whether a contents entry is fit to stand in as a product title.
func goodHeading(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" || badHeadings[strings.ToLower(t)] || endsDangling(t) {
		return false
	}
	words := strings.Fields(t)
	if len(words) > 8 {
		return false
	}
	real := 0
	for _, w := range words {
		lw := strings.ToLower(w)
		if strings.HasPrefix(lw, "p.") || lw == "pg" || lw == "pg." {
			return false
		}
		switch wordClass(strings.Trim(w, ",;:·—-")) {
		case "real":
			real++
		case "junk":
			if len(w) > 1 && !strings.ContainsAny(w, "·—/") {
				return false
			}
		}
	}
	return real >= 1 && real*2 >= len(words)
}

// RetitleCatalogFamilies gives every catalog-derived family the best description found on any
// price-book line for its SKUs (preferring "SKU description…" lines); when no line reads as
// prose it keeps a sensible existing title, else the page's contents heading, else the SKU.
func (s *Store) RetitleCatalogFamilies(ctx context.Context, brandCode string) (int, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT f.id, COALESCE(f.title,''), b.name, f.family_key,
		  i.sku, i.line, COALESCE(i.price_cents,0), i.catalog_id, i.page
		FROM product_families f
		JOIN brands b ON b.id=f.brand_id
		JOIN products p ON p.family_id=f.id
		JOIN catalogs c ON c.brand_code=b.code
		JOIN catalog_items i ON i.catalog_id=c.id AND i.sku=p.sku
		WHERE f.source='catalog' AND ($1='' OR b.code=$1)
		ORDER BY f.id`, brandCode)
	if err != nil {
		return 0, err
	}
	type fam struct {
		id                int64
		title, brand, key string
		best              string
		bestScore         int
		cat, pg           int
	}
	fams := map[int64]*fam{}
	var order []int64
	for rows.Next() {
		var id int64
		var title, brand, key, sku, line string
		var price int64
		var cat, pg int
		if err := rows.Scan(&id, &title, &brand, &key, &sku, &line, &price, &cat, &pg); err != nil {
			rows.Close()
			return 0, err
		}
		f := fams[id]
		if f == nil {
			f = &fam{id: id, title: title, brand: brand, key: key, bestScore: -1, cat: cat, pg: pg}
			fams[id] = f
			order = append(order, id)
		}
		t := trimTitle(cleanTitle(line, sku, price))
		sc := scoreTitle(t)
		if sc < 0 {
			continue
		}
		lu := strings.ToUpper(strings.TrimLeft(line, " .-|"))
		lu = strings.TrimPrefix(strings.TrimPrefix(lu, "ART. "), "ART ")
		if strings.HasPrefix(lu, strings.ToUpper(sku)) {
			sc += 15 // "SKU description" lines are the real product names
		} else if i := strings.Index(lu, strings.ToUpper(sku)); i > 40 && len(strings.Fields(lu[:i])) >= 6 {
			sc -= 20 // marketing copy that merely mentions the SKU at the end (sell sheets)
		}
		if sc > f.bestScore {
			f.best, f.bestScore, f.cat, f.pg = t, sc, cat, pg
		}
	}
	rows.Close()
	n := 0
	for _, id := range order {
		f := fams[id]
		title := f.best
		if title == "" {
			cur := f.title
			switch {
			case cur != "" && cur != f.brand && cur != f.key && hasRealWord(cur) && goodHeading(cur):
				title = cur
			default:
				if sec := s.sectionTitle(ctx, f.cat, f.pg); goodHeading(sec) && hasRealWord(sec) {
					title = sec
				} else {
					title = f.key
				}
			}
		}
		if title == f.title {
			continue
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE product_families SET title=$2, search=to_tsvector('english',$2||' '||family_key) WHERE id=$1`, f.id, title); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// seriesWord: the word after a one-letter line name ("M Series", "L Collection").
func seriesWord(w string) bool {
	switch strings.ToLower(strings.Trim(w, ",;:")) {
	case "series", "collection", "line":
		return true
	}
	return false
}
