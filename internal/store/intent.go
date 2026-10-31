package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Query understanding for the shop and header search. "silver faucet" is not two substrings:
// silver is a finish family (chrome, nickel, stainless, platinum…), faucet a product type with
// synonyms (mixer, lavatory set, widespread…). Concepts are ANDed; the patterns inside one
// concept are ORed. Unrecognised words stay as plain must-match tokens.

type Concept struct {
	Kind     string   // finish | type | word | sku
	Label    string   // what the user typed, for the "understood as" line
	Patterns []string // any of these substrings satisfies the concept
}

type Intent struct {
	Concepts []Concept
	Room     string   // kitchen | bathroom | hardware | appliances, when the query names one
	Brands   []string // brand slugs named in the query
	Labels   []string // brand names named in the query
}

func (in Intent) Empty() bool { return len(in.Concepts) == 0 && in.Room == "" && len(in.Brands) == 0 }

// finish families: the family word → finish-name substrings that belong to it
var finishFamilies = map[string][]string{
	"silver":      {"chrome", "nickel", "stainless", "steel", "platinum", "pewter", "silver", "aluminum", "aluminium", "titanium"},
	"silvers":     {"chrome", "nickel", "stainless", "steel", "platinum", "pewter", "silver"},
	"chrome":      {"chrome"},
	"nickel":      {"nickel"},
	"steel":       {"stainless", "steel"},
	"stainless":   {"stainless"},
	"gold":        {"gold", "brass", "champagne", "ormolu"},
	"golden":      {"gold", "brass", "champagne"},
	"brass":       {"brass"},
	"bronze":      {"bronze", "rubbed", "tuscan", "cocoa"},
	"black":       {"black", "ebony", "noir", "onyx", "carbon", "charcoal", "gun metal", "gunmetal", "graphite"},
	"white":       {"white", "bianco", "blanc"},
	"copper":      {"copper", "rose gold", "rosegold"},
	"rose":        {"rose gold", "rosegold", "copper"},
	"gray":        {"gray", "grey", "gun metal", "gunmetal", "graphite", "titanium", "pewter", "charcoal"},
	"grey":        {"gray", "grey", "gun metal", "gunmetal", "graphite", "titanium", "pewter", "charcoal"},
	"pewter":      {"pewter"},
	"brushed":     {"brushed", "satin"},
	"satin":       {"satin", "brushed"},
	"matte":       {"matte", "matt ", "flat "},
	"polished":    {"polished"},
	"antique":     {"antique", "aged", "vintage", "weathered", "distressed"},
	"aged":        {"aged", "antique", "vintage"},
	"unlacquered": {"unlacquered", "living"},
}

var finishLabels = map[string]string{
	"silver": "silver finishes (chrome, nickel, stainless, platinum)", "silvers": "silver finishes",
	"gold": "gold finishes (gold, brass, champagne)", "golden": "gold finishes", "black": "black finishes",
	"bronze": "bronze finishes", "white": "white finishes", "copper": "copper & rose gold", "rose": "rose gold & copper",
	"gray": "gray finishes", "grey": "gray finishes", "antique": "antique & aged finishes",
}

// product types and their synonyms (all matched as substrings of the family blob)
var typeSynonyms = map[string][]string{
	"faucet":       {"faucet", "mixer", "lavatory set", "widespread", "monobloc", "single hole", "centerset", "tap "},
	"tap":          {"faucet", "mixer", "tap "},
	"mixer":        {"mixer", "faucet"},
	"sink":         {"sink", "basin", "washbasin", "lavatory", "vessel"},
	"basin":        {"basin", "sink", "washbasin", "lavatory"},
	"lavatory":     {"lavatory", "basin", "sink"},
	"toilet":       {"toilet", "water closet", "wc ", "one-piece", "two-piece"},
	"bidet":        {"bidet"},
	"tub":          {"tub", "bath "},
	"bathtub":      {"bathtub", "tub"},
	"shower":       {"shower"},
	"showerhead":   {"showerhead", "shower head", "rain head", "rainhead", "raincan"},
	"handshower":   {"hand shower", "handshower", "handheld", "hand held"},
	"filler":       {"filler"},
	"potfiller":    {"pot filler"},
	"vanity":       {"vanity", "vanities", "console"},
	"mirror":       {"mirror"},
	"cabinet":      {"cabinet", "vanity"},
	"knob":         {"knob"},
	"pull":         {"pull", "handle"},
	"handle":       {"handle", "lever", "pull"},
	"lever":        {"lever"},
	"hinge":        {"hinge"},
	"lock":         {"lock", "deadbolt", "latch"},
	"deadbolt":     {"deadbolt"},
	"towel":        {"towel"},
	"hook":         {"hook"},
	"drain":        {"drain", "waste"},
	"disposal":     {"disposal", "disposer"},
	"filter":       {"filter", "filtration", "purif"},
	"dispenser":    {"dispenser", "soap"},
	"range":        {"range", "stove", "cooker"},
	"stove":        {"range", "stove"},
	"oven":         {"oven"},
	"cooktop":      {"cooktop", "rangetop", "hob"},
	"hood":         {"hood", "ventilation", "vent "},
	"refrigerator": {"refrigerat", "fridge"},
	"fridge":       {"refrigerat", "fridge"},
	"freezer":      {"freezer"},
	"dishwasher":   {"dishwasher"},
	"washer":       {"washer"},
	"dryer":        {"dryer"},
	"microwave":    {"microwave"},
	"wine":         {"wine"},
	"valve":        {"valve", "rough"},
	"trim":         {"trim"},
	"cartridge":    {"cartridge"},
	"spout":        {"spout"},
	"sprayer":      {"spray"},
	"aerator":      {"aerator"},
	"light":        {"light", "sconce", "lamp"},
	"heater":       {"heater", "warmer", "radiator"},
}

// two-word phrases recognised before single words
var phrases = map[string][]string{
	"pot filler":       {"pot filler"},
	"medicine cabinet": {"medicine cabinet", "mirror cabinet", "mirrored cabinet"},
	"mirror cabinet":   {"mirror cabinet", "medicine cabinet", "mirrored cabinet"},
	"hand shower":      {"hand shower", "handshower", "handheld"},
	"shower head":      {"showerhead", "shower head", "rain head", "raincan"},
	"rain head":        {"rain head", "rainhead", "raincan", "showerhead"},
	"tub filler":       {"tub filler", "tub spout", "roman tub"},
	"towel bar":        {"towel bar"},
	"towel ring":       {"towel ring"},
	"paper holder":     {"paper holder", "tissue holder", "toilet paper"},
	"bar faucet":       {"bar faucet", "prep faucet", "bar/prep"},
	"prep faucet":      {"prep faucet", "bar faucet", "bar/prep"},
	"wall mount":       {"wall mount", "wall-mount", "wall mounted"},
	"deck mount":       {"deck mount", "deck-mount", "deck mounted"},
	"single hole":      {"single hole", "single-hole", "monobloc"},
	"pull down":        {"pull-down", "pull down", "pulldown"},
	"pull out":         {"pull-out", "pull out", "pullout"},
	"free standing":    {"freestanding", "free standing", "floor mount"},
	"floor mount":      {"floor mount", "floor-mount", "freestanding"},
	"rose gold":        {"rose gold", "rosegold", "copper"},
	"gun metal":        {"gun metal", "gunmetal"},
	"oil rubbed":       {"oil rubbed", "rubbed bronze"},
	"satin nickel":     {"satin nickel", "brushed nickel"},
	"brushed nickel":   {"brushed nickel", "satin nickel"},
	"polished nickel":  {"polished nickel"},
	"polished chrome":  {"polished chrome", "chrome"},
	"matte black":      {"matte black", "flat black", "matt black"},
	"stainless steel":  {"stainless"},
	"water closet":     {"toilet", "water closet"},
}

var roomWords = map[string]string{
	"kitchen": "kitchen", "bathroom": "bathroom", "bath": "bathroom", "hardware": "hardware", "appliance": "appliances", "appliances": "appliances",
}

var stopWords = map[string]bool{"a": true, "an": true, "the": true, "in": true, "for": true, "with": true, "and": true, "of": true, "to": true, "or": true, "finish": true, "finishes": true, "color": true, "colour": true, "style": true}

func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "sses"), strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}

// ParseIntent turns a free-text query into concepts. brands maps lower-case brand name → slug.
func ParseIntent(q string, brands map[string]string) Intent {
	var in Intent
	lq := strings.ToLower(strings.TrimSpace(q))
	lq = strings.NewReplacer("“", "", "”", "", "\"", "", ",", " ", "/", " ").Replace(lq)
	if lq == "" {
		return in
	}
	// brand names first (may be several words); longest first so "Barber Wilsons" beats "Wilsons"
	var names []string
	for n := range brands {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, n := range names {
		if len(n) < 3 {
			continue
		}
		if i := strings.Index(" "+lq+" ", " "+n+" "); i >= 0 {
			in.Brands = append(in.Brands, brands[n])
			in.Labels = append(in.Labels, n)
			lq = strings.TrimSpace(strings.Replace(" "+lq+" ", " "+n+" ", " ", 1))
		}
	}
	// brand names typed with different spacing: "la cava" → lacava, "topknobs" → top knobs
	lq = strings.Join(mergeBrandWords(strings.Fields(lq), brands, &in), " ")
	words := strings.Fields(lq)
	for i := 0; i < len(words); i++ {
		w := strings.Trim(words[i], "-–—.:;()")
		if w == "" {
			continue
		}
		if i+1 < len(words) {
			bi := w + " " + strings.Trim(words[i+1], "-–—.:;()")
			if pats, ok := phrases[bi]; ok {
				in.Concepts = append(in.Concepts, Concept{Kind: kindFor(bi), Label: bi, Patterns: pats})
				i++
				continue
			}
		}
		if stopWords[w] {
			continue
		}
		if r, ok := roomWords[w]; ok && in.Room == "" {
			in.Room = r
			continue
		}
		s := singular(w)
		if pats, ok := finishFamilies[w]; ok {
			in.Concepts = append(in.Concepts, Concept{Kind: "finish", Label: w, Patterns: pats})
			continue
		}
		if pats, ok := finishFamilies[s]; ok {
			in.Concepts = append(in.Concepts, Concept{Kind: "finish", Label: w, Patterns: pats})
			continue
		}
		if pats, ok := typeSynonyms[s]; ok {
			in.Concepts = append(in.Concepts, Concept{Kind: "type", Label: w, Patterns: pats})
			continue
		}
		if looksLikeSKUToken(strings.ToUpper(w)) {
			n := normalizeSKU(w)
			in.Concepts = append(in.Concepts, Concept{Kind: "sku", Label: strings.ToUpper(w), Patterns: []string{w, n}})
			continue
		}
		in.Concepts = append(in.Concepts, Concept{Kind: "word", Label: w, Patterns: []string{w}})
	}
	return in
}

func kindFor(phrase string) string {
	switch phrase {
	case "rose gold", "gun metal", "oil rubbed", "satin nickel", "brushed nickel", "polished nickel", "polished chrome", "matte black", "stainless steel":
		return "finish"
	}
	return "type"
}

func normalizeSKU(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

// Explain is the "understood as" line: "faucets · silver finishes (chrome, nickel, …) · Brizo".
func (in Intent) Explain() string {
	var parts []string
	for _, c := range in.Concepts {
		switch c.Kind {
		case "finish":
			if l, ok := finishLabels[c.Label]; ok {
				parts = append(parts, l)
			} else {
				parts = append(parts, c.Label+" finish")
			}
		case "type":
			parts = append(parts, c.Label)
		case "sku":
			parts = append(parts, "SKU "+c.Label)
		default:
			parts = append(parts, "“"+c.Label+"”")
		}
	}
	for _, b := range in.Labels {
		parts = append(parts, strings.Title(b))
	}
	if in.Room != "" {
		parts = append(parts, in.Room)
	}
	return strings.Join(parts, " · ")
}

// regexAlt joins patterns into one case-insensitive alternation; pg_trgm answers it from the
// blob's trigram index (a chain of ORed ILIKEs forces a sequential scan).
func regexAlt(patterns []string) string {
	esc := make([]string, 0, len(patterns))
	for _, p := range patterns {
		esc = append(esc, regexp.QuoteMeta(p))
	}
	return "(" + strings.Join(esc, "|") + ")"
}

// conds renders the intent as SQL over the family blob (fb.blob) plus brand/room filters;
// anyWord ORs the concepts instead of ANDing them (last-resort widening).
func (in Intent) conds(args *[]any, anyWord bool) []string {
	return in.condsOpt(args, anyWord, true)
}

// HasFinish reports whether the query names a finish or finish family.
func (in Intent) HasFinish() bool {
	for _, c := range in.Concepts {
		if c.Kind == "finish" {
			return true
		}
	}
	return false
}

// HasTypeGroup reports whether a type concept resolves to known categories.
func (in Intent) HasTypeGroup() bool {
	for _, c := range in.Concepts {
		if c.Kind == "type" && len(CategoriesForType(c.Label)) > 0 {
			return true
		}
	}
	return false
}

// condsOpt: anyFinish also admits families whose finish is unknown ("finish:any").
func (in Intent) condsOpt(args *[]any, anyWord, anyFinish bool) []string {
	return in.condsStrict(args, anyWord, anyFinish, false)
}

// condsStrict: with strictType a type concept matches by category membership (plus blob text
// for families that have no category); otherwise by blob text alone.
func (in Intent) condsStrict(args *[]any, anyWord, anyFinish, strictType bool) []string {
	var groups []string
	for _, c := range in.Concepts {
		pats := c.Patterns
		if c.Kind == "finish" && anyFinish {
			pats = append(append([]string{}, pats...), "finish:any") // unknown finish: still a candidate
		}
		if cats := CategoriesForType(c.Label); strictType && c.Kind == "type" && len(cats) > 0 {
			*args = append(*args, cats, regexAlt(pats))
			groups = append(groups, fmt.Sprintf("(f.category_path && $%d OR (COALESCE(array_length(f.category_path,1),0)=0 AND fb.blob ~* $%d))", len(*args)-1, len(*args)))
			continue
		}
		*args = append(*args, regexAlt(pats))
		groups = append(groups, fmt.Sprintf("fb.blob ~* $%d", len(*args)))
	}
	var out []string
	if len(groups) > 0 {
		if anyWord {
			out = append(out, "("+strings.Join(groups, " OR ")+")")
		} else {
			out = append(out, groups...)
		}
	}
	if len(in.Brands) > 0 {
		*args = append(*args, in.Brands)
		out = append(out, fmt.Sprintf("b.slug = ANY($%d)", len(*args)))
	}
	if in.Room != "" {
		// soft: the family's room, or the word in its text ("kitchen faucet" under a bath brand)
		*args = append(*args, in.Room)
		out = append(out, fmt.Sprintf("(f.room = $%d OR fb.blob ~* $%d)", len(*args), len(*args)))
	}
	return out
}

// partTypes are the product types a shopper names when they really want a rough-in, trim or
// repair part; any other query ranks finished fixtures (tier 0) above parts (tier 2).
var partTypes = map[string]bool{
	"valve": true, "trim": true, "cartridge": true, "aerator": true, "spout": true, "handle": true,
	"lever": true, "part": true, "rough": true, "hose": true, "escutcheon": true, "flange": true, "stem": true,
}

// WantsParts reports whether the query names a part type (valve, cartridge, trim…).
func (in Intent) WantsParts() bool {
	for _, c := range in.Concepts {
		if (c.Kind == "type" || c.Kind == "word") && partTypes[singular(c.Label)] {
			return true
		}
	}
	return false
}

// relevance is an ORDER BY expression: type/word hits in the title outrank blob-only hits, and
// a finished fixture outranks a rough-in or part unless the shopper asked for parts. A finish
// match alone ("brushed nickel") is worth less than the fixture/part distinction, so a brushed
// nickel faucet whose finish is unknown still beats a rough valve that lists -BN.
func (in Intent) relevance(args *[]any) string {
	var terms []string
	if !in.WantsParts() {
		terms = append(terms, "((2 - f.tier) * 4)")
	}
	for _, c := range in.Concepts {
		w := 2
		if c.Kind == "type" {
			w = 4
		}
		if c.Kind == "finish" {
			// a family that really comes in the finish outranks one whose finish is unknown
			*args = append(*args, regexAlt(c.Patterns))
			terms = append(terms, fmt.Sprintf("(CASE WHEN fb.blob ~* $%d THEN 3 ELSE 0 END)", len(*args)))
			continue
		}
		*args = append(*args, regexAlt(c.Patterns))
		terms = append(terms, fmt.Sprintf("(CASE WHEN f.title ~* $%d THEN %d ELSE 0 END)", len(*args), w))
		if cats := CategoriesForType(c.Label); c.Kind == "type" && len(cats) > 0 {
			// the curated category outranks a title word: "Bathroom Sink Faucets" beats a tub
			// whose title says "for use with faucets"
			*args = append(*args, cats)
			terms = append(terms, fmt.Sprintf("(CASE WHEN f.category_path && $%d THEN 6 ELSE 0 END)", len(*args)))
		}
	}
	if len(terms) == 0 {
		return "0"
	}
	return strings.Join(terms, " + ")
}

// mergeBrandWords matches brand names whose spacing differs from the query: two or three
// adjacent words whose concatenation is a brand key ("la cava", "sub zero"), or one word that
// equals a multi-word brand name with its spaces removed ("topknobs"). Matched words are
// removed and the brand recorded on in.
func mergeBrandWords(words []string, brands map[string]string, in *Intent) []string {
	if len(brands) == 0 {
		return words
	}
	var compact map[string]string // "topknobs" → "top knobs"
	for n := range brands {
		if strings.Contains(n, " ") {
			if compact == nil {
				compact = map[string]string{}
			}
			compact[strings.ReplaceAll(n, " ", "")] = n
		}
	}
	var out []string
	for i := 0; i < len(words); i++ {
		matched := false
		for span := 3; span >= 2; span-- {
			if i+span > len(words) {
				continue
			}
			joined := strings.Join(words[i:i+span], "")
			if len(joined) < 5 {
				continue
			}
			if slug, ok := brands[joined]; ok {
				in.Brands = append(in.Brands, slug)
				in.Labels = append(in.Labels, joined)
				i += span - 1
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if n, ok := compact[words[i]]; ok && len(words[i]) >= 5 {
			in.Brands = append(in.Brands, brands[n])
			in.Labels = append(in.Labels, n)
			continue
		}
		out = append(out, words[i])
	}
	return out
}

// brand name → slug map, cached for five minutes
var (
	brandNamesMu   sync.Mutex
	brandNamesAt   time.Time
	brandNamesMemo map[string]string
)

func (s *Store) brandNames(ctx context.Context) map[string]string {
	brandNamesMu.Lock()
	defer brandNamesMu.Unlock()
	if time.Since(brandNamesAt) < 5*time.Minute && brandNamesMemo != nil {
		return brandNamesMemo
	}
	m := map[string]string{}
	rows, err := s.Pool.Query(ctx, `SELECT lower(name), slug, lower(COALESCE(code,'')) FROM brands`)
	if err == nil {
		for rows.Next() {
			var n, slug, code string
			if rows.Scan(&n, &slug, &code) == nil {
				m[n] = slug
				// "california faucets" is also asked for as "california"; keep single-word prefixes
				// only when they are distinctive (≥ 6 letters)
				if f := strings.Fields(n); len(f) > 1 && len(f[0]) >= 6 && !typeWord(f[0]) && roomWords[f[0]] == "" && finishFamilies[f[0]] == nil {
					if _, taken := m[f[0]]; !taken {
						m[f[0]] = slug
					}
				}
			}
		}
		rows.Close()
	}
	brandNamesMemo, brandNamesAt = m, time.Now()
	return m
}

func typeWord(w string) bool {
	_, ok := typeSynonyms[singular(w)]
	return ok
}
