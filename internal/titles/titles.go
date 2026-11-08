// Package titles synthesises display titles for families whose printed title is unusable
// on a card: a bare model number ("SH-PB1-15-033"), or a page heading shared by dozens of
// rows ("SOLID BRASS", "NORDIC COLLECTION"). The printed title stays in families.title;
// the synthesized one goes to display_title and the index and pages read COALESCE of both.
package titles

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Input is what is known about one family.
type Input struct {
	Title       string
	Category    string
	Collection  string
	SKU         string
	SpecText    string            // first page of the maker's spec sheet, when fetched
	Description string            // the maker's copy, when the feed carries it
	Facts       map[string]string // typed facts / site attributes
	Shared      int               // families of the brand that print the same title
}

var (
	reCode     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 ./-]{2,27}$`)
	reDigit    = regexp.MustCompile(`\d`)
	reSpecType = regexp.MustCompile(`(?i)product type\s*[:\-]?\s*(.{4,90}?)\s+(features|specifications|specification|job name|model|dimensions|description|$)`)
	reSpaces   = regexp.MustCompile(`\s+`)
	reTrailing = regexp.MustCompile(`[\s.,;:\-]+$`)
)

// Codey reports whether a title is a model number rather than a name.
func Codey(title, sku string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return true
	}
	if strings.EqualFold(t, strings.TrimSpace(sku)) {
		return true
	}
	return reCode.MatchString(t) && reDigit.MatchString(t) && !strings.Contains(t, " ") || len(t) < 4
}

var (
	reDims      = regexp.MustCompile(`^\s*\d+(\.\d+)?\s*[xX]\s*\d+`)
	reVowel     = regexp.MustCompile(`[aeiouyAEIOUY]`)
	reArtifact  = regexp.MustCompile(`andapos;|&apos;|&#39;`)
	reClauseSep = regexp.MustCompile(`[.;:]\s|\s-\s|\s\|\s`)
)

// Abbreviated reports whether a title reads like a warehouse line ("36 X 18 X 34.5 Montlake
// Profile Decorative Vanity Shaker Choc Cherry LH", "6315 2.5BS B/B MRTS LCK"): it opens with
// dimensions, or a third of its words are short vowel-less codes.
func Abbreviated(title string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return false
	}
	if reDims.MatchString(t) {
		return true
	}
	words := strings.Fields(t)
	if len(words) < 3 {
		return false
	}
	codes, readable := 0, 0
	for _, w := range words {
		w = strings.Trim(w, "().,/")
		if w == "" {
			continue
		}
		hasDigit := strings.IndexFunc(w, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
		short := len(w) <= 3 && !reVowel.MatchString(w)
		shouted := len(w) >= 4 && w == strings.ToUpper(w) && !reVowel.MatchString(w)
		switch {
		case hasDigit || short || shouted:
			codes++
		case len(w) >= 4 && reVowel.MatchString(w) && w != strings.ToUpper(w):
			readable++
		}
	}
	// a spec-heavy but readable name ("… Heat Pump, 650 cfm, 208 to 230 VAC") stands
	return float64(codes) >= 0.35*float64(len(words)) && readable < 5
}

// descriptionTitle takes the first clause of the maker's copy as a title when it reads like a
// product name: a handful of words, mixed case, no sentence filler.
func descriptionTitle(desc string) string {
	d := reArtifact.ReplaceAllString(strings.TrimSpace(desc), "'")
	d = reSpaces.ReplaceAllString(d, " ")
	if d == "" {
		return ""
	}
	if loc := reClauseSep.FindStringIndex(d); loc != nil && loc[0] >= 12 {
		d = d[:loc[0]]
	}
	d = strings.TrimRight(d, " .,;:-")
	words := strings.Fields(d)
	if len(words) < 2 || len(words) > 14 || len(d) > 90 {
		return ""
	}
	lower := strings.ToLower(d)
	for _, bad := range []string{"this ", "these ", "our ", "your ", "we ", "is a ", "are a ", "features", "designed to", "perfect for"} {
		if strings.Contains(lower, bad) {
			return ""
		}
	}
	if !reVowel.MatchString(d) || d == strings.ToUpper(d) {
		return ""
	}
	return d
}

// Synthesize returns a display title, or "" when the printed title should stand.
func Synthesize(in Input) string {
	// a warehouse-style line gives way to the maker's own sentence when the feed has one
	if Abbreviated(in.Title) && !Codey(in.Title, in.SKU) {
		if t := descriptionTitle(in.Description); t != "" && !strings.EqualFold(t, in.Title) {
			return clip(t)
		}
	}
	codey := Codey(in.Title, in.SKU)
	// a title printed on four or more families of the brand is a page heading or a material
	// line, not a product name, when it is short or shouted; a long descriptive title that
	// repeats across finish variants stands as it is
	shared := in.Shared >= 4 && (len(strings.Fields(in.Title)) <= 3 || strings.ToUpper(in.Title) == in.Title)
	if !codey && !shared {
		return ""
	}
	sku := strings.TrimSpace(in.SKU)
	if sku == "" && codey {
		sku = strings.TrimSpace(in.Title) // the printed code is the only identifier there is
	}
	ptype := productType(in)
	single := Singular(in.Category)
	switch {
	case ptype != "" && codey:
		return clip(join(ptype, sku))
	case ptype != "" && shared:
		return clip(join(ptype, sku))
	case single != "" && codey && in.Collection != "" && !strings.EqualFold(in.Collection, single):
		return clip(join(TitleCase(in.Collection), single, sku))
	case single != "" && codey:
		return clip(join(single, sku))
	case single != "" && shared && !genericCategory[in.Category] && !strings.Contains(strings.ToLower(in.Title), strings.ToLower(lastWord(single))):
		return clip(join(TitleCase(in.Title), single, sku))
	case shared && sku != "":
		return clip(TitleCase(in.Title) + " · " + sku)
	}
	return ""
}

// productType reads the maker's own type line from a spec sheet or the facts.
func productType(in Input) string {
	for _, k := range []string{"Product Type", "Type", "Product", "Description"} {
		if v := strings.TrimSpace(in.Facts[k]); len(v) >= 4 && len(v) <= 90 && !Codey(v, in.SKU) {
			return clean(v)
		}
	}
	if m := reSpecType.FindStringSubmatch(in.SpecText); m != nil {
		return clean(m[1])
	}
	return ""
}

func clean(s string) string {
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	s = reTrailing.ReplaceAllString(s, "")
	if s == strings.ToUpper(s) {
		s = TitleCase(s)
	} else if r := []rune(s); len(r) > 0 && unicode.IsLower(r[0]) {
		r[0] = unicode.ToUpper(r[0])
		s = string(r)
	}
	return s
}

// genericCategory names buckets that add nothing to a title ("Edward Privacy Door Hardware").
var genericCategory = map[string]bool{"Door Hardware": true, "Faucet Parts": true, "Bathtub Parts": true, "Bath Accessories": true,
	"Kitchen Accessories": true, "Drains": true, "Parts": true, "Shower Systems": true, "Hardware": true}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	return f[len(f)-1]
}

func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func clip(s string) string {
	if len(s) > 90 {
		s = s[:90]
		if i := strings.LastIndex(s, " "); i > 40 {
			s = s[:i]
		}
	}
	return s
}

// TitleCase turns an all-caps heading into title case; mixed-case input is returned as is.
func TitleCase(s string) string {
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")
	if s != strings.ToUpper(s) {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		switch w {
		case "and", "or", "of", "the", "with", "for", "in", "on", "&":
			if i > 0 {
				continue
			}
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

var singulars = map[string]string{
	"Deadbolts & Locks": "Deadbolt", "Towel Bars & Rings": "Towel Bar", "Paper Holders": "Paper Holder", "Bath Accessories": "Bath Accessory",
	"Kitchen Accessories": "Kitchen Accessory", "Faucet Parts": "Faucet Part", "Bathtub Parts": "Tub Part", "Rough-In Valves": "Rough-In Valve",
	"Shower Faucet Trims": "Shower Trim", "Pressure Balance Valve Trims": "Pressure Balance Trim", "Hand Shower Slide Bars": "Slide Bar",
	"Roman Tub Faucets With Hand Showers": "Roman Tub Faucet", "Door Hardware": "Door Hardware", "Medicine Cabinets": "Medicine Cabinet",
	"Wine Storage": "Wine Storage", "Laundry": "Laundry", "Water Filtration": "Water Filter", "Lighting": "Light", "Shower Systems": "Shower System",
	"Body Sprays": "Body Spray", "Cabinet Pulls": "Cabinet Pull", "Handle Sets": "Handle Set", "Door Stops & Bumpers": "Door Stop",
	"Bar & Prep Faucets": "Bar Faucet", "Pot Filler Faucets": "Pot Filler", "Garbage Disposals": "Garbage Disposal", "Towel Warmers": "Towel Warmer",
}

// Singular turns a category name into a product noun ("Kitchen Faucets" → "Kitchen Faucet").
func Singular(cat string) string {
	cat = strings.TrimSpace(cat)
	if cat == "" {
		return ""
	}
	if s, ok := singulars[cat]; ok {
		return s
	}
	switch {
	case strings.HasSuffix(cat, "ies"):
		return cat[:len(cat)-3] + "y"
	case strings.HasSuffix(cat, "ches") || strings.HasSuffix(cat, "shes") || strings.HasSuffix(cat, "sses"):
		return cat[:len(cat)-2]
	case strings.HasSuffix(cat, "s") && !strings.HasSuffix(cat, "ss"):
		return cat[:len(cat)-1]
	}
	return cat
}

// Build recomputes display_title for every brand (or one). Returns rows changed.
func Build(ctx context.Context, pool *pgxpool.Pool, brandCode string, dry bool) (int, error) {
	rows, err := pool.Query(ctx, `
		WITH shared AS (SELECT brand_id, title, count(*) n FROM families WHERE tier <> 3 GROUP BY 1, 2)
		SELECT f.id, COALESCE(f.title,''), COALESCE(f.category,''), COALESCE(f.collection,''), f.display_title, COALESCE(f.description,''),
		       COALESCE((SELECT i.sku FROM items i WHERE i.family_id=f.id ORDER BY (i.price_cents IS NOT NULL) DESC, i.sku LIMIT 1), ''),
		       COALESCE((SELECT left(d.text, 1500) FROM documents d WHERE d.family_id=f.id AND d.kind='spec' AND d.text IS NOT NULL ORDER BY d.id LIMIT 1), ''),
		       f.facts, f.attributes, COALESCE(s.n, 1)
		FROM families f JOIN brands b ON b.id=f.brand_id
		LEFT JOIN shared s ON s.brand_id=f.brand_id AND s.title=f.title
		WHERE f.tier <> 3 AND ($1 = '' OR b.code = $1)`, brandCode)
	if err != nil {
		return 0, err
	}
	type upd struct {
		id    int64
		title *string
	}
	var ups []upd
	n := 0
	for rows.Next() {
		var id int64
		var title, cat, coll, desc, sku, spec string
		var cur *string
		var facts, attrs map[string]any
		var shared int
		if err := rows.Scan(&id, &title, &cat, &coll, &cur, &desc, &sku, &spec, &facts, &attrs, &shared); err != nil {
			rows.Close()
			return 0, err
		}
		in := Input{Title: title, Category: cat, Collection: coll, SKU: sku, SpecText: spec, Description: desc, Shared: shared, Facts: map[string]string{}}
		for k, v := range attrs {
			if s, ok := v.(string); ok {
				in.Facts[k] = s
			}
		}
		for k, v := range facts {
			if m, ok := v.(map[string]any); ok {
				if s, ok := m["value"].(string); ok {
					in.Facts[k] = s
				}
			}
		}
		got := Synthesize(in)
		var want *string
		if got != "" && got != title {
			want = &got
		}
		if (want == nil) != (cur == nil) || (want != nil && *want != *cur) {
			ups = append(ups, upd{id, want})
			if dry && n < 40 {
				fmt.Printf("%d  %q → %q\n", id, title, got)
			}
			n++
		}
	}
	rows.Close()
	if dry {
		return n, nil
	}
	batch := &pgx.Batch{}
	for _, u := range ups {
		batch.Queue(`UPDATE families SET display_title=$2, updated_at=now() WHERE id=$1`, u.id, u.title)
	}
	br := pool.SendBatch(ctx, batch)
	for range ups {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	slog.Info("titles built", "changed", n, "brand", brandCode)
	return n, nil
}
