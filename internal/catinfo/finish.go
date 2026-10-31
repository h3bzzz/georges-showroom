package catinfo

import (
	"regexp"
	"sort"
	"strings"
)

// finishNames are the finish and colour names the books print; matching is case-insensitive
// and longest-first so "Polished Chrome" never yields "Chrome". Single-word names are only
// those that are unambiguous on their own (Chrome, White, Biscuit…); "Brass", "Steel",
// "Black" alone are materials or options words far more often than finishes.
var finishNames = []string{
	// chrome / nickel
	"Polished Chrome", "Chrome", "Satin Chrome", "Brushed Chrome", "Matte Chrome", "Polished Nickel", "Satin Nickel", "Brushed Nickel",
	"Matte Nickel", "Antique Nickel", "Luxe Nickel", "Black Nickel", "Burnished Nickel", "Aged Nickel", "Polished Nickel Uncoated", "Burnished Nickel Uncoated",
	"Steelnox Satin Nickel", "Vibrant Brushed Nickel", "Vibrant Polished Nickel", "Lifetime Polished Nickel", "Lifetime Satin Nickel",
	// brass / gold
	"Polished Brass", "Satin Brass", "Brushed Brass", "Antique Brass", "Unlacquered Brass", "Non-Lacquered Brass", "Aged Brass", "Burnished Brass",
	"Vintage Brass", "Vintage Brushed Brass", "Tuscan Brass", "Inca Brass", "Italian Brass", "English Brass", "French Brass", "Elite Brass", "Rustica Brass",
	"Polished Brass Uncoated", "Burnished Brass Uncoated", "Polished Brass PVD", "Satin Brass PVD", "Forever Brass PVD", "Lifetime Polished Brass",
	"Polished Natural Brass", "Natural Brass", "Weathered Brass", "Brushed Brass PVD", "Matte Brass",
	"Polished Gold", "Satin Gold", "Brushed Gold", "Rose Gold", "French Gold", "Luxe Gold", "24K Gold", "Matte Gold", "Antique Gold", "Champagne Gold",
	"Matte British Gold PVD", "British Gold", "Lifetime Polished Gold", "Lifetime Satin Gold", "Satin Gold PVD", "French Gold PVD", "Vibrant Brushed Gold",
	"Vibrant French Gold", "Vibrant Rose Gold", "Polished Gold PVD", "Brushed Gold PVD", "Satin English Gold", "Soft Gold",
	// bronze / copper / dark
	"Oil Rubbed Bronze", "Oil-Rubbed Bronze", "Venetian Bronze", "English Bronze", "Antique Bronze", "Satin Bronze", "Brushed Bronze", "Matte Bronze",
	"Tuscan Bronze", "Olive Bronze", "Warm Bronze", "Aged Bronze", "Weathered Bronze", "Bella Terra Bronze", "Mocha Bronze", "Chocolate Bronze",
	"Distressed Oil Rubbed Bronze", "Distressed Venetian Bronze", "Distressed Antique Nickel", "Rustico Bronze", "Flat Black Bronze", "Dark Bronze",
	"Medium Bronze Patina", "Tumbled White Bronze", "White Bronze", "Saint Bronze", "Satin Bronze PVD", "Vibrant Brushed Bronze", "Oil Rubbed Bronze PVD",
	"Polished Copper", "Antique Copper", "Brushed Copper", "Matte Copper PVD", "Matte Copper", "Vintage Copper", "Antique Copper Flat", "Antique Brass Flat",
	"Antique Nickel Flat", "Matte Black", "Matt Black", "Flat Black", "Satin Black", "Enduring Matt Black", "Black Onyx", "Gunmetal", "Gun Metal",
	"Brushed Gunmetal", "Matte Gun Metal PVD", "Matte Gunmetal PVD", "Matte Gun Metal", "Graphite", "Carbon", "Titanium", "Pewter", "Charcoal", "Ebony",
	"Black Metal", "Onyx", "Slate", "Cocoa Bronze", "Platinum", "Polished Platinum", "Satin Platinum",
	// steel / stainless
	"Stainless Steel", "Brushed Stainless Steel", "Polished Stainless Steel", "Matte Stainless Steel", "Satin Stainless Steel", "Ultra Stainless Steel",
	"Stainless Steel PVD", "Luxe Steel", "Steel", "Brushed Stainless", "Polished Stainless",
	// colours (ceramic, acrylic, composite)
	"White", "Matte White", "Gloss White", "Glossy White", "Soft White", "Biscuit", "Bisque", "Bone", "Almond", "Linen", "Ice Grey", "Ice Gray", "Dune",
	"Coal Black", "Anthracite", "Cinder", "Volcano Gray", "Truffle", "Café", "Cafe", "Metallic Gray", "Concrete Gray", "Rock Gray", "Steel Gray",
	"Silver", "Sterling Silver", "Polished Silver", "Satin Silver", "Nickel Silver", "Green Patina", "Verde",
	"Satin 24K Gold", "Polished 24K Gold", "Satin Elite Brass", "Satin PVD Brass", "PVD Brass", "Polished Inca", "Satin Inca", "Brushed Inca", "Satin Brass And Black",
	"Luxe Gold / Teak", "Polished Nickel / Teak", "Luxe Nickel / Teak", "Matte Black / Luxe Gold", "Matte Black / Luxe Steel", "Luxe Nickel / Matte Black",
	"Polished Antique", "Antique Pewter", "Satin Pewter", "Matte Pewter", "Oil Rubbed Brass", "Lacquered Brass", "Polished Lacquered Brass",
}

var (
	finishCanon map[string]string
	reFinish    *regexp.Regexp
)

func init() {
	finishCanon = map[string]string{}
	names := append([]string(nil), finishNames...)
	sort.SliceStable(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	var alts []string
	for _, n := range names {
		finishCanon[strings.ToLower(n)] = n
		alts = append(alts, regexp.QuoteMeta(n))
	}
	reFinish = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])(` + strings.Join(alts, "|") + `)(?:[^A-Za-z]|$)`)
}

// canonFinish maps spelling variants onto one name.
var canonFinish = map[string]string{
	"Oil-Rubbed Bronze": "Oil Rubbed Bronze", "Matt Black": "Matte Black", "Gun Metal": "Gunmetal", "Cafe": "Café", "Ice Gray": "Ice Grey",
	"Glossy White": "Gloss White", "Matte Gunmetal PVD": "Matte Gun Metal PVD", "Non-Lacquered Brass": "Unlacquered Brass",
}

// Finishes returns the distinct finish names printed in s, in order of appearance.
func Finishes(s string) []string {
	var out []string
	seen := map[string]bool{}
	pos := 0
	for pos < len(s) {
		loc := reFinish.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		name := finishCanon[strings.ToLower(s[pos+loc[2]:pos+loc[3]])]
		if c, ok := canonFinish[name]; ok {
			name = c
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		pos += loc[3]
	}
	return out
}

// IsFinish reports whether s is exactly a finish name.
func IsFinish(s string) bool {
	_, ok := finishCanon[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

// CanonFinish returns the canonical spelling of a finish name ("" when unknown).
func CanonFinish(s string) string {
	n, ok := finishCanon[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return ""
	}
	if c, ok := canonFinish[n]; ok {
		return c
	}
	return n
}

// blockFinishes drops names that are materials in context: "stainless steel sink",
// "solid brass", "white vitreous china" are not finish choices.
var reFinishAsMaterial = regexp.MustCompile(`(?i)(stainless steel|steel|brass|white|black)\s+(sink|construction|body|cartridge|vitreous|ceramic|fireclay|composite|only|or)`)

func blockFinishes(s string) []string {
	var out []string
	for _, f := range Finishes(s) {
		if f == "Steel" || f == "Silver" || f == "Carbon" || f == "Onyx" || f == "Slate" || f == "Verde" || f == "Linen" || f == "Dune" {
			continue // too often a plain word in a feature line
		}
		if reFinishAsMaterial.MatchString(s) && (f == "Stainless Steel" || f == "White" || f == "Steel") {
			continue
		}
		out = append(out, f)
	}
	return out
}
