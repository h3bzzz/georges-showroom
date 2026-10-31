package ocr

import "strings"

// Canon repairs OCR-mangled SKUs ("AD0BWU", "AO0SWU") to the clean form seen elsewhere in the
// same book ("A008WU"). Feed every page through Add first, then map tokens through Fix.
type Canon struct {
	count  map[string]int
	onPage map[int]map[string]bool
	byLen  map[int][]string
}

func NewCanon() *Canon {
	return &Canon{count: map[string]int{}, onPage: map[int]map[string]bool{}, byLen: map[int][]string{}}
}

// Add records the SKU tokens of one page.
func (c *Canon) Add(page int, words []Word) {
	if c.onPage[page] == nil {
		c.onPage[page] = map[string]bool{}
	}
	for _, w := range words {
		if LooksLikeSKU(w.Text) && w.Conf >= 50 {
			t := Normalize(w.Text)
			c.count[t]++
			c.onPage[page][t] = true
		}
	}
}

// finish freezes the trusted set: SKUs seen at least twice, or once with no confusable rival.
func (c *Canon) finish() {
	if len(c.byLen) > 0 {
		return
	}
	for t, n := range c.count {
		if n >= 2 {
			c.byLen[len(t)] = append(c.byLen[len(t)], t)
		}
	}
}

// Fix returns the trusted SKU a token most likely is, or the token itself. A trusted SKU
// printed on the same page may differ in up to three confusable characters (the header row
// is clean, the small finish rows below it are not); one elsewhere in the book in two.
func (c *Canon) Fix(page int, tok string) string {
	c.finish()
	t := Normalize(tok)
	if c.count[t] >= 2 {
		return t
	}
	best, bestN := "", 0
	for _, cand := range c.byLen[len(t)] {
		limit := 2
		if c.onPage[page][cand] {
			limit = 3
		}
		if confusable(t, cand, limit) && c.count[cand] > bestN {
			best, bestN = cand, c.count[cand]
		}
	}
	if best != "" {
		return best
	}
	return t
}

var confusePairs = map[[2]byte]bool{}

func init() {
	groups := []string{"0ODQ", "8B3S5", "1IL|", "2Z", "6G", "4A"}
	for _, g := range groups {
		for i := 0; i < len(g); i++ {
			for j := 0; j < len(g); j++ {
				confusePairs[[2]byte{g[i], g[j]}] = true
			}
		}
	}
}

// confusable: same length, at most `limit` positions differ, and every difference is a
// classic OCR swap (0/O/D, 8/B/S/5, 1/I/L …).
func confusable(a, b string, limit int) bool {
	if len(a) != len(b) || a == b {
		return false
	}
	diff := 0
	for i := 0; i < len(a); i++ {
		if a[i] == b[i] {
			continue
		}
		if !confusePairs[[2]byte{a[i], b[i]}] {
			return false
		}
		diff++
		if diff > limit {
			return false
		}
	}
	// don't let a numeric-only token drift onto a lettered SKU by luck: require a shared letter
	return strings.ContainsAny(b, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || diff == 1
}

// Resolve maps a SKU-looking word to a usable SKU. Confident reads pass through Fix; shaky
// reads (conf < 50) are kept only when they repair to a SKU the book prints cleanly.
func (c *Canon) Resolve(page int, w Word) (string, bool) {
	fixed := c.Fix(page, w.Text)
	if !LooksLikeSKU(w.Text) {
		return fixed, fixed != Normalize(w.Text) && c.count[fixed] >= 2 // shape-only: repaired or dropped
	}
	if w.Conf >= 50 {
		return fixed, true
	}
	return fixed, c.count[fixed] >= 2
}
