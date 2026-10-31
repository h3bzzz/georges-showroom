package site

import (
	"sort"
	"strings"
)

const productPrefix = "/Pasadena-California/"

// FixURL repairs the doubled host the site sometimes emits in canonical links
// ("https://www.georgesshowroom.comhttps://www.georgesshowroom.com/…").
func FixURL(u string) string {
	u = strings.TrimSpace(u)
	for strings.Contains(u, BaseURL+"https://") || strings.Contains(u, BaseURL+"http://") {
		u = strings.Replace(u, BaseURL+"https://", "https://", 1)
		u = strings.Replace(u, BaseURL+"http://", "http://", 1)
	}
	return u
}

// IsProductURL reports whether u is a SKU page (…/Pasadena-California/<Brand>-<SKU>-<Finish>-<Category>.HTM).
func IsProductURL(u string) bool {
	return strings.Contains(u, productPrefix) && strings.HasSuffix(strings.ToLower(u), ".htm")
}

// ProductSlug returns the page name of a product URL without the .HTM suffix
// ("Native-Trails-NSL3619-CX-Charcoal-Drop-In-Bathroom-Sink").
func ProductSlug(u string) string {
	i := strings.Index(u, productPrefix)
	if i < 0 {
		return ""
	}
	s := u[i+len(productPrefix):]
	if j := strings.IndexAny(s, "?#"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, ".HTM"), ".htm")
}

// BrandMatcher finds the brand slug a product URL starts with. Slugs are compared
// case-insensitively and the longest match wins ("Barclay-collections" before "Barclay").
type BrandMatcher struct {
	slugs []string // sorted longest first
	lower []string
}

func NewBrandMatcher(slugs []string) *BrandMatcher {
	m := &BrandMatcher{slugs: append([]string(nil), slugs...)}
	sort.SliceStable(m.slugs, func(i, j int) bool { return len(m.slugs[i]) > len(m.slugs[j]) })
	m.lower = make([]string, len(m.slugs))
	for i, s := range m.slugs {
		m.lower[i] = strings.ToLower(s) + "-"
	}
	return m
}

// Match returns the brand slug (as stored in the brands table) and the remainder of the page
// name ("NSL3619-CX-Charcoal-Drop-In-Bathroom-Sink"); ok=false when no brand matches.
func (m *BrandMatcher) Match(u string) (slug, rest string, ok bool) {
	name := ProductSlug(u)
	if name == "" {
		return "", "", false
	}
	ln := strings.ToLower(name)
	for i, l := range m.lower {
		if strings.HasPrefix(ln, l) {
			return m.slugs[i], name[len(l):], true
		}
	}
	return "", name, false
}
