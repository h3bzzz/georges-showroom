package search

import (
	"regexp"
	"strings"

	"georges/internal/taxonomy"
)

// Intent is what a query asks for, read before touching the index: a brand, a product type,
// a finish, maybe a model number, and whatever words remain for full text.
type Intent struct {
	Raw        string
	Brand      string   // brand slug
	Categories []string // from type words/phrases
	Finish     *taxonomy.FinishAsk
	SKU        string // a model-number-looking token
	Text       string // remaining words
}

var reSKUish = regexp.MustCompile(`^[A-Za-z]*\d[A-Za-z0-9./-]*$`)

// BrandIndex resolves query words to brand slugs: lower-cased name and slug, spaces removed.
type BrandIndex struct {
	bySlug map[string]string
	names  map[string]string // normalized name → slug
}

func NewBrandIndex(slugs, names []string) *BrandIndex {
	b := &BrandIndex{bySlug: map[string]string{}, names: map[string]string{}}
	for i, s := range slugs {
		b.bySlug[strings.ToLower(s)] = s
		b.names[normBrand(names[i])] = s
		b.names[normBrand(s)] = s
	}
	return b
}

func normBrand(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// Parse reads a query. Brand names may span two words ("newport brass", "california
// faucets"); finish phrases are runs of finish vocabulary ("brushed nickel", "matte black").
func Parse(q string, brands *BrandIndex) Intent {
	in := Intent{Raw: strings.TrimSpace(q)}
	words := strings.Fields(strings.ToLower(in.Raw))
	used := make([]bool, len(words))
	// brands: longest span first (3, 2, 1 words)
	if brands != nil {
	outer:
		for span := 3; span >= 1; span-- {
			for i := 0; i+span <= len(words); i++ {
				if slug, ok := brands.names[normBrand(strings.Join(words[i:i+span], ""))]; ok {
					in.Brand = slug
					for k := i; k < i+span; k++ {
						used[k] = true
					}
					break outer
				}
			}
		}
	}
	// type phrases, then type words
	for i := 0; i+1 < len(words); i++ {
		if used[i] || used[i+1] {
			continue
		}
		if cats := taxonomy.CategoriesForType(words[i] + " " + words[i+1]); cats != nil {
			in.Categories = append(in.Categories, cats...)
			used[i], used[i+1] = true, true
		}
	}
	for i, w := range words {
		if used[i] {
			continue
		}
		if cats := taxonomy.CategoriesForType(w); cats != nil && in.Categories == nil {
			in.Categories = append(in.Categories, cats...)
			used[i] = true
		}
	}
	// finish phrase: a run that starts with an anchor or a leading modifier and continues
	// through finish vocabulary
	for i := 0; i < len(words); i++ {
		if used[i] || !taxonomy.FinishStart(words[i]) {
			continue
		}
		j := i
		anchored := false
		for j < len(words) && !used[j] && taxonomy.FinishVocab(words[j]) {
			if taxonomy.FinishAnchor(words[j]) {
				anchored = true
			}
			j++
		}
		if anchored {
			ask := taxonomy.AskFinish(words[i:j])
			in.Finish = &ask
			for k := i; k < j; k++ {
				used[k] = true
			}
			break
		}
	}
	var rest []string
	for i, w := range words {
		if used[i] {
			continue
		}
		if in.SKU == "" && reSKUish.MatchString(w) && len(w) >= 3 {
			in.SKU = strings.ToUpper(w)
			continue
		}
		rest = append(rest, w)
	}
	in.Text = strings.Join(rest, " ")
	return in
}
