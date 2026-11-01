// Package toc derives a table of contents for a catalog from OCR word boxes: big, short text
// near the top of a page is a section heading; consecutive pages under the same heading fold
// into one entry.
package toc

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"georges_/internal/ocr"
)

type Entry struct {
	Page  int
	Title string
	Level int
}

var (
	reDigits = regexp.MustCompile(`\d`)
	reJunk   = regexp.MustCompile(`[^A-Za-z0-9&'’\-/ ]`)
)

// PageHeading returns the most likely heading on one page, or "".
func PageHeading(words []ocr.Word, pageH int) string {
	if len(words) < 3 {
		return ""
	}
	hs := make([]int, 0, len(words))
	for _, w := range words {
		if w.Conf >= 60 && w.H > 4 {
			hs = append(hs, w.H)
		}
	}
	if len(hs) < 3 {
		return ""
	}
	sort.Ints(hs)
	median := hs[len(hs)/2]
	minH := max(int(float64(median)*1.9), 22)

	type key struct{ b, p, l int }
	lines := map[key][]ocr.Word{}
	for _, w := range words {
		if w.Conf < 60 || w.H < minH {
			continue
		}
		k := key{w.Block, w.Par, w.Line}
		lines[k] = append(lines[k], w)
	}
	best, bestScore := "", 0.0
	for _, ws := range lines {
		sort.Slice(ws, func(i, j int) bool { return ws[i].X < ws[j].X })
		var parts []string
		avgH, y := 0, ws[0].Y
		for _, w := range ws {
			parts = append(parts, w.Text)
			avgH += w.H
		}
		avgH /= len(ws)
		t := strings.TrimSpace(reJunk.ReplaceAllString(strings.Join(parts, " "), " "))
		t = strings.Join(strings.Fields(t), " ")
		letters := 0
		for _, r := range t {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				letters++
			}
		}
		if letters < 4 || len(t) > 48 || len(t) < 3 || ocr.LooksLikeSKU(t) {
			continue
		}
		short, bad := 0, false
		for _, w := range strings.Fields(t) {
			if len(w) == 1 {
				short++
			}
			if mixedDigits(w) || tripleRun(w) || strings.EqualFold(w, "com") || strings.EqualFold(w, "www") {
				bad = true
			}
		}
		if short > 0 || bad || len(strings.Fields(t)) > 7 {
			continue // "G Ssi", "5lack", "llllf", "gessi com", long sentences
		}
		if float64(len(reDigits.FindAllString(t, -1))) > float64(len(t))*0.3 {
			continue // price columns, dimensions
		}
		// score: bigger and higher on the page wins
		score := float64(avgH) * (1.0 - 0.5*float64(y)/float64(max(pageH, 1)))
		if score > bestScore {
			bestScore, best = score, t
		}
	}
	return titleCase(best)
}

// mixedDigits flags OCR confusions like "5lack" or "Finishesl2".
func mixedDigits(w string) bool {
	d, l := 0, 0
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			d++
		case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			l++
		}
	}
	return d > 0 && l > 0
}

// tripleRun flags "llllf"-style garbage.
func tripleRun(w string) bool {
	lw := strings.ToLower(w)
	for i := 2; i < len(lw); i++ {
		if lw[i] == lw[i-1] && lw[i] == lw[i-2] {
			return true
		}
	}
	return false
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	up := strings.ToUpper(s) == s
	if !up {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Options steer Fold with what the backend already knows about a brand.
type Options struct {
	Brand       string          // brand name: a page that only says "Lacava" is a cover, not a section
	Collections []string        // collection names from the brand's families: a heading naming one is a section for sure
	Dict        map[string]bool // lowercase English words (LoadDict); nil = no dictionary
}

// LoadDict reads a words file (one per line) into a lowercase set. An empty path or a
// missing file gives nil, and Fold then asks a lone unknown word to recur in the book.
func LoadDict(path string) map[string]bool {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	d := make(map[string]bool, 100000)
	for _, w := range strings.Split(string(b), "\n") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && !strings.Contains(w, "'") {
			d[w] = true
		}
	}
	return d
}

// connectors may stay lowercase inside a title ("Innovation for the Kitchen", "Jason Wu for Brizo").
var connectors = map[string]bool{"of": true, "and": true, "for": true, "the": true, "by": true, "with": true, "&": true,
	"de": true, "di": true, "da": true, "la": true, "le": true, "del": true, "della": true, "e": true, "in": true, "on": true, "to": true, "a": true}

// sentenceLike flags body text read as a title: a later word in lowercase that is not a
// connector ("Effortless harmony", "Delivers near boiling water on dem"). The raw heading is
// checked, not the key, since the key is lowercased.
func sentenceLike(h string) bool {
	ws := strings.Fields(h)
	for i, w := range ws {
		if i == 0 {
			continue
		}
		r := rune(w[0])
		if r >= 'a' && r <= 'z' && !connectors[strings.ToLower(reJunk.ReplaceAllString(w, ""))] {
			return true
		}
	}
	return false
}

// garbled flags OCR debris: a token of one or two letters that is not a connector or an
// initial ("Chrome Cr", "Bell iS", "Exits /3"), a word with a case flip inside
// ("soFTENER"), a hyphen left dangling ("High-"), or a 15-letter run ("forcoordinating").
func garbled(h string) bool {
	ws := strings.Fields(h)
	for i, w := range ws {
		bare := reJunk.ReplaceAllString(w, "")
		if bare == "" {
			return true
		}
		// a one- or two-letter token is debris unless it is a connector, or a capitalised
		// name in the middle of a title ("Jason Wu For Brizo"); "Chrome Cr", "Rough Ct",
		// "Ct Chrome Cr" and "Bell iS" all fail
		if n := letters(bare); n <= 2 && !connectors[strings.ToLower(bare)] {
			cap := bare[0] >= 'A' && bare[0] <= 'Z'
			if !cap || i == 0 || i == len(ws)-1 || n == 1 {
				return true
			}
		}
		if strings.HasSuffix(w, "-") || len(bare) > 14 {
			return true
		}
		upper, lower := 0, 0
		for i := 0; i < len(bare); i++ {
			switch {
			case bare[i] >= 'A' && bare[i] <= 'Z':
				upper++
			case bare[i] >= 'a' && bare[i] <= 'z':
				lower++
			}
			if i > 0 && bare[i-1] >= 'a' && bare[i-1] <= 'z' && bare[i] >= 'A' && bare[i] <= 'Z' {
				return true
			}
		}
		if upper >= 3 && lower >= 1 {
			return true // "OTHERi", "SOFTENERs": an all-caps word with a stray lowercase letter
		}
	}
	return false
}

// CleanTitle trims a printed-index line to its title: everything from the first token with
// no letters or digits ("¢ % : s", "© oo", ". i 8§ 3 i") is OCR debris from dot leaders.
func CleanTitle(t string) string {
	var keep []string
	for _, w := range strings.Fields(t) {
		if w == "·" || w == "|" || w == "-" || w == "–" || w == "—" {
			keep = append(keep, w)
			continue
		}
		bare := reJunk.ReplaceAllString(w, "")
		if letters(bare) == 0 && !strings.ContainsAny(bare, "0123456789") {
			break
		}
		if len(bare) == 1 && bare >= "a" && bare <= "z" {
			break
		}
		keep = append(keep, w)
	}
	out := strings.TrimRight(strings.Join(keep, " "), " .,:;-·|")
	return strings.TrimSpace(out)
}

// stopHeadings are big words that open a page without naming a section: the spec column
// headers, finish names and marketing verbs the OCR picks up when a price page has no title.
var stopHeadings = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`with for and the only model models description standard technology installation
		deposits metal steel nickel chrome brass bronze black white gold plate list func zoom towel book system upgrade
		replacement seal spout temperature page contents price prices note notes index new features specifications specs
		dimensions sign create matter poetry neutral nature rain rail exits cold hot no yes everything com inc llc terms
		warranty table height width depth push pair pull extends finished material materials colour color colours colors
		options option size sizes style styles design designs collection collections series product products overview
		reimagining innovation inspiring quality performance available order ordering shipping freight program
		hole wall will function supply universal assure matte other satin brushed polished rubbed oil only also
		important please information info details detail description descriptions example examples`) {
		stopHeadings[w] = true
	}
}

// leadStop are words a section name never starts with.
var leadStop = map[string]bool{"with": true, "for": true, "and": true, "the": true, "are": true, "or": true, "to": true, "of": true,
	"in": true, "by": true, "w": true, "is": true, "a": true, "an": true, "from": true, "at": true}

// groupWords name a department rather than a collection: Kitchen, Bath Accessories, Sinks.
var groupWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`kitchen kitchens bath baths bathroom bathrooms hardware accessories accessory shower showers
		tub tubs bathtub bathtubs faucet faucets sink sinks toilet toilets lighting appliances appliance outdoor commercial parts
		components drains drain finishes finish cabinet cabinets door doors vanities vanity mirrors mirror lavatory lavatories
		bidets bidet urinals consoles trims trim valves valve rough-in rough-ins fillers`) {
		groupWords[w] = true
	}
}

var groupGlue = map[string]bool{"collections": true, "collection": true, "products": true, "fixtures": true, "and": true, "&": true, "the": true}

func normKey(s string) string {
	s = strings.ToLower(reJunk.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

// IsGroup reports whether a heading names a department ("Kitchen", "Bath Collections",
// "Kitchen Sinks") rather than a collection.
func IsGroup(key string) bool {
	ws := strings.Fields(key)
	if len(ws) == 0 || len(ws) > 3 {
		return false
	}
	hit := false
	for _, w := range ws {
		switch {
		case groupWords[w]:
			hit = true
		case groupGlue[w]:
		default:
			return false
		}
	}
	return hit
}

// matchCollection returns the longest known collection the heading starts or ends with.
func matchCollection(key string, known map[string]string) string {
	best := ""
	for k := range known {
		if key == k || strings.HasPrefix(key, k+" ") || strings.HasSuffix(key, " "+k) {
			if len(k) > len(best) {
				best = k
			}
		}
	}
	if best == "" {
		return ""
	}
	return known[best]
}

// acceptable is the junk fence for a heading with no collection behind it. freq is how many
// pages of the book open with this heading.
func acceptable(h, key string, freq int, opt Options) bool {
	if key == "" || strings.Contains(key, "contents") || key == normKey(opt.Brand) {
		return false
	}
	r := rune(h[0])
	if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
		return false // "installation", "deposits", "wand spray and": body text, not a title
	}
	if sentenceLike(h) || garbled(h) {
		return false
	}
	ws := strings.Fields(key)
	if len(ws) > 6 || leadStop[ws[0]] {
		return false
	}
	if len(ws) == 1 {
		w := ws[0]
		if stopHeadings[w] || letters(w) < 4 {
			return false
		}
		// a lone word that is neither English nor a department: "Ventaglio" names a collection
		// and heads several pages; "Oncrete" and "Ermostat" are misreads that occur once
		if !groupWords[w] && !(opt.Dict != nil && opt.Dict[w]) && freq < 2 {
			return false
		}
	}
	allStop := true
	for i, w := range ws {
		if !stopHeadings[w] && !leadStop[w] {
			allStop = false
		}
		if i > 0 && w == ws[i-1] {
			return false // "No No"
		}
	}
	return !allStop
}

// wordsContained reports whether every word of the shorter key appears in the longer.
func wordsContained(a, b string) bool {
	wa, wb := strings.Fields(a), strings.Fields(b)
	if len(wa) > len(wb) {
		wa, wb = wb, wa
	}
	if len(wa) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, w := range wb {
		set[w] = true
	}
	for _, w := range wa {
		if !set[w] {
			return false
		}
	}
	return true
}

// Fold turns per-page headings into entries: the first page of each section wins, running
// headers that repeat across much of the book are dropped, a heading that merely restates
// the previous entry a few pages on ("Kintsu Kitchen" p.28, "Kintsu" p.31) is folded into
// it, and junk that is not a title is refused unless it names one of the brand's known
// collections. Department headings are level 1, everything under them level 2.
func Fold(headings map[int]string, pages int, opt Options) []Entry {
	known := map[string]string{}
	for _, c := range opt.Collections {
		if k := normKey(c); letters(k) >= 3 && !IsGroup(k) && !stopHeadings[k] {
			known[k] = c
		}
	}
	freq := map[string]int{}
	for _, h := range headings {
		if h != "" {
			freq[normKey(h)]++
		}
	}
	var out []Entry
	seen := map[string]bool{}
	lastColl, lastCollPage := "", 0
	for p := 1; p <= pages; p++ {
		h := headings[p]
		if h == "" {
			continue
		}
		key := normKey(h)
		if seen[key] {
			continue // first occurrence of a section wins; later repeats are running headers
		}
		if pages >= 20 && freq[key] > pages/4 {
			continue
		}
		coll := matchCollection(key, known)
		if coll == "" && !acceptable(h, key, freq[key], opt) {
			continue
		}
		seen[key] = true
		if n := len(out); n > 0 && p-out[n-1].Page <= 8 && wordsContained(key, normKey(out[n-1].Title)) {
			continue
		}
		// "Baliza Bath" p.116 then "Baliza" p.127: the same collection, still its section
		if coll != "" && coll == lastColl && p-lastCollPage <= 16 {
			continue
		}
		if coll != "" {
			lastColl, lastCollPage = coll, p
		}
		title := h
		if coll != "" && len(strings.Fields(key)) == len(strings.Fields(normKey(coll))) {
			title = coll
		}
		level := 2
		if coll == "" && IsGroup(key) {
			level = 1
		}
		out = append(out, Entry{Page: p, Title: title, Level: level})
	}
	// no departments at all: a flat list reads better than one indented from its first line
	hasGroup := false
	for _, e := range out {
		if e.Level == 1 {
			hasGroup = true
		}
	}
	if !hasGroup {
		for i := range out {
			out[i].Level = 1
		}
	}
	return out
}
