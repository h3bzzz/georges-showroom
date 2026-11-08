package taxonomy

import (
	"sort"
	"strings"
)

// Finish understanding. A finish name is a tone (the metal or colour: bronze, nickel, brass…)
// plus a treatment (polished, satin, matte, antique, unlacquered) and brand modifiers that
// carry no meaning for a shopper (PVD, Lifetime, Vibrant, Luxe). Every distinct finish in
// products is classified once into those parts; a query's finish words are classified the same
// way and resolved to the list of real finish names that share the tone and do not contradict
// the treatment. "antique rubbed bronze" is therefore Oil Rubbed Bronze, Antique Bronze,
// Venetian Bronze, Distressed Oil Rubbed Bronze… and never a chrome faucet whose text happens
// to contain "bronze". This is the canonical-value-plus-synonyms pattern every mature catalog
// search uses (a colour family facet over normalised values), applied to finishes.

// toneWords maps a word in a finish name or query to its tone.
var toneWords = map[string]string{
	"chrome": "chrome", "chrom": "chrome",
	"nickel": "nickel", "nickle": "nickel",
	"stainless": "stainless", "steel": "stainless", "inox": "stainless",
	"silver": "silver", "platinum": "silver", "titanium": "silver", "aluminum": "silver", "aluminium": "silver",
	"pewter": "pewter",
	"brass":  "brass",
	"gold":   "gold", "golden": "gold", "24k": "gold", "champagne": "gold", "inca": "gold",
	"bronze": "bronze", "tuscan": "bronze", "venetian": "bronze",
	"copper": "copper",
	"black":  "black", "ebony": "black", "onyx": "black", "noir": "black", "coal": "black", "carbon": "black", "anthracite": "black", "cinder": "black",
	"white": "white", "bisque": "white", "biscuit": "white", "bone": "white", "pearl": "white", "ivory": "white", "blanc": "white", "bianco": "white",
	"gray": "gray", "grey": "gray", "gunmetal": "gray", "gun": "gray", "metal": "gray", "graphite": "gray", "charcoal": "gray", "slate": "gray", "concrete": "gray", "volcano": "gray", "metallic": "gray", "ash": "gray",
	"green": "green", "verde": "green",
	"brown": "brown", "café": "brown", "cafe": "brown", "truffle": "brown", "earth": "brown", "mocha": "brown", "chocolate": "brown", "cocoa": "brown", "coco": "brown",
}

// toneGroups are the shopper's broad colour words: each stands for several tones.
var toneGroups = map[string][]string{
	"silver":   {"chrome", "nickel", "stainless", "silver", "pewter"},
	"gold":     {"gold", "brass"},
	"gray":     {"gray", "pewter"},
	"grey":     {"gray", "pewter"},
	"gunmetal": {"gray"},
	"dark":     {"bronze", "black", "gray"},
	"rose":     {"copper"},
}

// treatmentWords maps a word to its treatment.
var treatmentWords = map[string]string{
	"polished": "polished", "bright": "polished", "shiny": "polished", "gloss": "polished", "glossy": "polished",
	"satin": "satin", "brushed": "satin", "burnished": "satin",
	"matte": "matte", "matt": "matte", "flat": "matte", "honed": "matte",
	"antique": "antique", "antiqued": "antique", "aged": "antique", "rubbed": "antique", "oil": "antique", "oiled": "antique",
	"distressed": "antique", "weathered": "antique", "vintage": "antique", "tumbled": "antique", "patina": "antique",
	"rustic": "antique", "rustico": "antique", "rustica": "antique", "venetian": "antique", "tuscan": "antique",
	"unlacquered": "unlacquered", "uncoated": "unlacquered", "living": "unlacquered", "raw": "unlacquered",
}

// leadingModifiers may open a finish phrase ("dark bronze", "pvd brass"); other modifiers
// only continue one, so "in matte black" starts at matte.
var leadingModifiers = map[string]bool{"dark": true, "warm": true, "light": true, "pvd": true, "lifetime": true, "vibrant": true, "luxe": true, "soft": true}

// finishModifiers carry no meaning for a shopper and are dropped from names and queries.
var finishModifiers = map[string]bool{
	"pvd": true, "lifetime": true, "vibrant": true, "luxe": true, "elite": true, "max": true, "ultra": true, "forever": true,
	"natural": true, "plated": true, "finish": true, "finishes": true, "color": true, "colour": true, "tone": true, "look": true,
	"and": true, "&": true, "/": true, "in": true, "with": true, "the": true, "a": true, "dark": true, "warm": true, "light": true,
	"moderne": true, "standard": true, "sable": true, "cannon": true, "honey": true, "shaded": true, "midnight": true, "soft": true,
	"english": true, "french": true, "medium": true, "timeless": true, "saint": true, "bella": true, "terra": true, "lacquered": true, "non": true,
}

// FinishEntry is one real finish name, classified.
type FinishEntry struct {
	Name      string
	Tones     []string // every tone in the name ("Matte Black / Luxe Gold" has two)
	Head      string   // the tone the finish reads as: a leading colour word (Black Nickel is black) or else the last metal named (White Bronze is bronze… but see Head())
	TwoTone   bool     // "Matte Black / Luxe Gold": either tone is a fair match
	Treatment string   // polished | satin | matte | antique | unlacquered | "" (unspecified)
	Tokens    []string // canonical tokens: tones + treatment, sorted, no modifiers
	Count     int      // SKUs carrying it
}

// FinishAsk is what a query's finish words ask for.
type FinishAsk struct {
	Label     string   // the words as typed
	Tones     []string // tones asked for (a group word expands to several)
	Treatment string
	Tokens    []string // canonical tokens, for exact-name scoring
	Group     bool     // a colour-family word (silver, gold, gray) was expanded: no single name is "exact"
}

// ClassifyFinish splits a finish name into tones, treatment and canonical tokens.
func ClassifyFinish(name string) FinishEntry {
	e := FinishEntry{Name: name}
	a := classifyFinishWords(splitFinish(name))
	e.Tones, e.Treatment, e.Tokens = a.Tones, a.Treatment, a.Tokens
	ln := strings.ToLower(name)
	e.TwoTone = strings.Contains(ln, "/") || strings.Contains(ln, " and ") || strings.Contains(ln, "&")
	if len(a.Tones) > 0 {
		// "Black Nickel", "White Bronze": a colour word first names the look; otherwise the
		// last metal named is the head noun ("Nickel Silver" is silver-coloured)
		e.Head = a.Tones[len(a.Tones)-1]
		if first := a.Tones[0]; first == "black" || first == "white" {
			e.Head = first
		}
	}
	return e
}

func splitFinish(s string) []string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("-", " ", "/", " ", "&", " ", ",", " ", "(", " ", ")", " ", "“", "", "”", "", "\"", "").Replace(s)
	return strings.Fields(s)
}

// classifyFinishWords is shared by names and queries. Tones keep first-seen order; the
// treatment is the first one named (a "Brushed Satin Nickel" is satin either way).
func classifyFinishWords(words []string) FinishAsk {
	var a FinishAsk
	seen := map[string]bool{}
	for i, w := range words {
		if w == "rose" && i+1 < len(words) && words[i+1] == "gold" {
			// rose gold is a copper tone, not a gold one
			if !seen["copper"] {
				a.Tones = append(a.Tones, "copper")
				seen["copper"] = true
			}
			words[i+1] = ""
			continue
		}
		if w == "" {
			continue
		}
		if w == "non" && i+1 < len(words) && words[i+1] == "lacquered" {
			if a.Treatment == "" {
				a.Treatment = "unlacquered"
			}
			continue
		}
		if t, ok := toneWords[w]; ok {
			if !seen[t] {
				a.Tones = append(a.Tones, t)
				seen[t] = true
			}
		}
		if t, ok := treatmentWords[w]; ok && t != "" && a.Treatment == "" {
			a.Treatment = t
		}
	}
	a.Tokens = append(append([]string{}, a.Tones...), "")
	a.Tokens = a.Tokens[:len(a.Tones)]
	if a.Treatment != "" {
		a.Tokens = append(a.Tokens, a.Treatment)
	}
	sort.Strings(a.Tokens)
	return a
}

// finishVocab reports whether a word belongs to finish language (tone, treatment, group or
// modifier), so a run of such words in a query can be read as one finish phrase.
func FinishVocab(w string) bool {
	if _, ok := toneWords[w]; ok {
		return true
	}
	if _, ok := treatmentWords[w]; ok {
		return true
	}
	if _, ok := toneGroups[w]; ok {
		return true
	}
	return finishModifiers[w]
}

// finishStart reports whether a finish phrase may begin with this word.
func FinishStart(w string) bool { return FinishAnchor(w) || leadingModifiers[w] }

// finishAnchor reports whether a word alone justifies reading a phrase as a finish (a tone or
// a treatment; "dark" or "pvd" on their own do not).
func FinishAnchor(w string) bool {
	if _, ok := toneWords[w]; ok {
		return true
	}
	if _, ok := toneGroups[w]; ok && w != "dark" {
		return true
	}
	t, ok := treatmentWords[w]
	return ok && t != ""
}

// askFor reads a query's finish phrase. Group words (silver, gold, gray) expand to their
// tones; a tone word named outright stays specific ("bronze" is only bronze).
func AskFinish(words []string) FinishAsk {
	a := classifyFinishWords(append([]string{}, words...))
	a.Label = strings.Join(words, " ")
	seen := map[string]bool{}
	for _, t := range a.Tones {
		seen[t] = true
	}
	for i, w := range words {
		if g, ok := toneGroups[w]; ok {
			if w == "gold" && i > 0 && words[i-1] == "rose" {
				continue
			}
			for _, t := range g {
				if !seen[t] {
					a.Tones = append(a.Tones, t)
					seen[t] = true
				}
			}
			if w == "silver" || w == "gold" || w == "gray" || w == "grey" {
				a.Group = true
			}
		}
	}
	return a
}

// FinishDict is every finish name in products, classified.
type FinishDict struct {
	Entries []FinishEntry
	byName  map[string]*FinishEntry
}

// NewFinishDict classifies names (with SKU counts).
func NewFinishDict(counts map[string]int) *FinishDict {
	d := &FinishDict{byName: map[string]*FinishEntry{}}
	for n, c := range counts {
		e := ClassifyFinish(n)
		e.Count = c
		d.Entries = append(d.Entries, e)
	}
	sort.Slice(d.Entries, func(i, j int) bool {
		if d.Entries[i].Count != d.Entries[j].Count {
			return d.Entries[i].Count > d.Entries[j].Count
		}
		return d.Entries[i].Name < d.Entries[j].Name
	})
	for i := range d.Entries {
		d.byName[strings.ToLower(d.Entries[i].Name)] = &d.Entries[i]
	}
	return d
}

// Entry looks a finish name up.
func (d *FinishDict) Entry(name string) *FinishEntry {
	if d == nil {
		return nil
	}
	return d.byName[strings.ToLower(name)]
}

// Resolution is the real finish names an ask stands for. Exact are the names whose canonical
// tokens equal the ask's (Oil Rubbed Bronze, Antique Bronze and Venetian Bronze are all
// "antique bronze"); Names is every name that shares a tone and does not contradict the
// treatment (Dark Bronze has none, so it is in; Satin Bronze is out), exact ones first.
type Resolution struct {
	Exact []string
	Names []string
}

// Resolve lists the finish names an ask matches, most-stocked first within each rank.
func (d *FinishDict) Resolve(a FinishAsk) Resolution {
	var r Resolution
	if d == nil {
		return r
	}
	type cand struct {
		name  string
		score float64
		count int
	}
	var cands []cand
	for _, e := range d.Entries {
		if len(e.Tones) == 0 && e.Treatment == "" {
			continue
		}
		if len(a.Tones) > 0 && !sharesTone(a.Tones, e) {
			continue
		}
		if a.Treatment != "" && e.Treatment != "" && e.Treatment != a.Treatment {
			continue
		}
		if len(a.Tones) == 0 && e.Treatment != a.Treatment {
			continue // "antique" alone: only finishes that are antique
		}
		score := jaccard(a.Tokens, e.Tokens)
		if a.Group {
			score = 0 // "silver": Polished Chrome (6,240 SKUs) leads, not Platinum
		}
		cands = append(cands, cand{e.Name, score, e.Count})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].count > cands[j].count
	})
	for _, c := range cands {
		r.Names = append(r.Names, c.name)
		if c.score >= 0.99 {
			r.Exact = append(r.Exact, c.name)
		}
	}
	return r
}

func sharesTone(ask []string, e FinishEntry) bool {
	for _, a := range ask {
		if a == e.Head {
			return true
		}
		if e.TwoTone {
			for _, h := range e.Tones {
				if a == h {
					return true
				}
			}
		}
	}
	return false
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	inter := 0
	for _, y := range b {
		if set[y] {
			inter++
		}
	}
	union := len(set)
	for _, y := range b {
		if !set[y] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// ToneGroup folds a tone into what a photograph can show: chrome, nickel and stainless all
// read as silver to the camera and to CLIP. These are the vtone classes of catalog_crops.
func ToneGroup(tone string) string {
	switch tone {
	case "chrome", "nickel", "stainless", "silver", "pewter":
		return "silver"
	case "brass", "gold":
		return "gold"
	case "bronze", "brown":
		return "bronze"
	case "black", "white", "copper":
		return tone
	case "gray":
		return "silver" // brushed steel and gunmetal are one class to the camera
	}
	return "other"
}

// ToneGroups folds a list of tones, deduplicated.
func ToneGroups(tones []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tones {
		g := ToneGroup(t)
		if !seen[g] {
			out = append(out, g)
			seen[g] = true
		}
	}
	return out
}

// ToneLabel is how a tone group is named on a card ("pictured in silver").
func ToneLabel(group string) string {
	switch group {
	case "silver":
		return "chrome / nickel"
	case "gold":
		return "brass / gold"
	}
	return group
}
