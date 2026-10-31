package ocr

import (
	"sort"
	"strings"

	"georges_/internal/pricing"
)

// Line is one OCR text line with its SKU-looking tokens and a trailing price if present.
type Line struct {
	Text       string
	X, Y       int
	SKUs       []Word
	PriceCents int64
}

// Lines groups words into printed rows (words whose vertical extents overlap, regardless of
// the tesseract block they landed in, so "A008WU … Single-control bidet mixer … $1,787.00"
// comes back as one line) and picks out SKUs and the price nearest to the right of them.
//
// Prices are read with pricing.ParseCents (a dollar sign or a cents part is required) and never
// from an index page, whose "Model No. Page" columns otherwise read as money.
func Lines(words []Word) []Line {
	index := IsIndexPage(words)
	ws := make([]Word, 0, len(words))
	for _, w := range words {
		if strings.TrimSpace(w.Text) != "" && w.H > 0 {
			ws = append(ws, w)
		}
	}
	sort.Slice(ws, func(i, j int) bool {
		if ws[i].Y+ws[i].H/2 != ws[j].Y+ws[j].H/2 {
			return ws[i].Y+ws[i].H/2 < ws[j].Y+ws[j].H/2
		}
		return ws[i].X < ws[j].X
	})
	var rows [][]Word
	for _, w := range ws {
		yc := w.Y + w.H/2
		placed := false
		for i := len(rows) - 1; i >= 0 && i >= len(rows)-4; i-- {
			r := rows[i]
			ry, rh := rowCenter(r)
			tol := rh * 45 / 100
			if tol < 4 {
				tol = 4
			}
			if abs(yc-ry) <= tol {
				rows[i] = append(r, w)
				placed = true
				break
			}
		}
		if !placed {
			rows = append(rows, []Word{w})
		}
	}
	var out []Line
	for _, r := range rows {
		sort.Slice(r, func(i, j int) bool { return r[i].X < r[j].X })
		var parts []string
		ln := Line{X: r[0].X, Y: r[0].Y}
		for _, w := range r {
			parts = append(parts, w.Text)
			// shaky or shape-only reads are kept for Canon.Resolve to repair or drop; a money token
			// ("610.00") is SKU-shaped too but is never a SKU
			if !pricing.LooksLikeMoney(w.Text) && (LooksLikeSKU(w.Text) || skuShaped(w.Text)) {
				ln.SKUs = append(ln.SKUs, w)
			}
		}
		if len(ln.SKUs) == 0 {
			continue
		}
		ln.Text = strings.Join(parts, " ")
		if !index {
			ln.PriceCents = linePrice(r, ln.SKUs)
		}
		out = append(out, ln)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Y < out[j].Y })
	return out
}

func rowCenter(r []Word) (yc, h int) {
	sy, sh := 0, 0
	for _, w := range r {
		sy += w.Y + w.H/2
		sh += w.H
	}
	return sy / len(r), sh / len(r)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// linePrice is the first money token to the right of the last SKU, else the right-most money
// token on the row. A token that is itself one of the row's SKUs is never a price (Gessi and
// Brizo use all-digit model numbers), and a row that is a run of SKUs with no description is
// an index or cross-reference row, which carries no price at all.
//
// Two printed forms need help: a dollar sign OCR'd as its own word ("$ 1,787.00", Aboutwater,
// Hydro Systems) is glued back on; and a whole-dollar price with no sign or cents ("… polished
// chrome 1095", California Faucets) is accepted only as the last token of a row that carries a
// prose description after its SKU.
func linePrice(row []Word, skus []Word) int64 {
	isSKU := map[string]bool{}
	for _, k := range skus {
		isSKU[k.Text] = true
	}
	money := func(i int) int64 {
		w := row[i]
		if isSKU[w.Text] {
			return 0
		}
		t := w.Text
		if i > 0 && strings.Trim(row[i-1].Text, " ") == "$" {
			t = "$" + t
		}
		c, ok := pricing.ParseCents(t)
		if !ok {
			return 0
		}
		return c
	}
	lastSKU := skus[len(skus)-1].X
	for i, w := range row {
		if w.X > lastSKU {
			if c := money(i); c > 0 {
				return c
			}
		}
	}
	for i := len(row) - 1; i >= 0; i-- {
		if c := money(i); c > 0 {
			return c
		}
	}
	// bare whole dollars: last token, after a description of at least two words
	last := row[len(row)-1]
	if isSKU[last.Text] || last.X <= lastSKU {
		return 0
	}
	prose := 0
	for _, w := range row {
		if w.X > lastSKU && w.X < last.X && len(w.Text) >= 3 && isAlphaWord(w.Text) {
			prose++
		}
	}
	if prose < 2 {
		return 0
	}
	c, _ := pricing.ParseBareDollars(last.Text)
	return c
}

// IsIndexPage recognises a numerical / alphabetical index or cross-reference page: the word
// "index" in the top band of the page, or a "Model No. … Page" header, or a page that is mostly
// SKU-shaped tokens with hardly any prose. Such pages list page numbers next to SKUs, which
// would otherwise be read as prices and become product titles.
func IsIndexPage(words []Word) bool {
	if len(words) < 20 {
		return false
	}
	maxY := 0
	for _, w := range words {
		if w.Y+w.H > maxY {
			maxY = w.Y + w.H
		}
	}
	top := maxY / 6
	skuish, prose, money, pageHdr, modelHdr := 0, 0, 0, 0, 0
	for _, w := range words {
		t := strings.Trim(w.Text, ",.;:()")
		lt := strings.ToLower(t)
		if w.Y < top {
			if lt == "index" || strings.HasSuffix(lt, "index") {
				return true
			}
		}
		switch lt {
		case "page", "pg", "pg.":
			pageHdr++
		case "model", "model#", "no", "no.", "item", "article", "art.", "code", "number":
			modelHdr++
		}
		switch {
		case pricing.LooksLikeMoney(w.Text):
			money++
		case LooksLikeSKU(t) || skuShaped(t):
			skuish++
		case len(t) >= 4 && isAlphaWord(t):
			prose++
		}
	}
	// a price table (Brizo, Chicago Faucets) is also dense with SKUs and thin on prose, but it is
	// full of money tokens; an index has none
	if money*10 >= skuish && money > 0 {
		return false
	}
	if pageHdr >= 2 && modelHdr >= 2 {
		return true
	}
	return skuish >= 40 && prose*4 < skuish
}

func isAlphaWord(t string) bool {
	for _, r := range t {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '\'') {
			return false
		}
	}
	return true
}

// skuShaped: an upper-case alphanumeric token that could be a mangled SKU ("ADOBWU" for
// A008WU). Such tokens are never used as-is; Canon.Resolve must repair them to a trusted SKU.
func skuShaped(t string) bool {
	t = strings.Trim(t, ",;:()[]")
	if len(t) < 5 || len(t) > 16 {
		return false
	}
	for _, r := range t {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
