// Package specs fetches manufacturers' specification sheets by model number. Each brand
// that publishes sheets at a derivable URL gets a resolver (verified 2026-11-07 by fetching
// a real sheet); brands behind JavaScript portals or logins are left to a later browser pass.
package specs

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Candidate is one URL worth asking for.
type Candidate struct {
	URL  string
	Name string // file name to store under data/raw/docs/<CODE>/
}

// Resolver turns a SKU into candidate sheet URLs, most likely first. finishes are the brand's
// known finish codes (suffixes) so the model base can be cut cleanly; category is the
// family's merchandised category when the brand needs it (Brizo's room letter).
type Resolver func(sku, category string, finishes map[string]bool) []Candidate

// Resolvers by Unilog brand code.
var Resolvers = map[string]Resolver{
	"CAL": calFaucets,
	"CHI": chicago,
	"NEW": newportBrass,
	"KOL": kohler,
	"KLL": kallista,
	"LAV": lacava,
	"BAL": baldwin,
	"BRZ": brizo,
}

var reFinishTail = regexp.MustCompile(`[-/.][A-Z0-9]{1,5}$`)

// base strips a finish suffix: a known code first, else a short trailing token.
func base(sku string, finishes map[string]bool) string {
	s := strings.ToUpper(strings.TrimSpace(sku))
	for _, sep := range []string{"-", "/", "."} {
		if i := strings.LastIndex(s, sep); i > 0 {
			if finishes[s[i+1:]] {
				return s[:i]
			}
		}
	}
	return s
}

func calFaucets(sku, _ string, fin map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	b := base(s, fin)
	var out []Candidate
	add := func(model string) {
		out = append(out, Candidate{fmt.Sprintf("https://images.calfaucets.com/technical/%s_sp.pdf", url.PathEscape(model)), model + "_sp.pdf"})
	}
	if b != s {
		add(b + "-XX") // K30-123-XX_sp.pdf: the finish placeholder stands in the file name
	}
	add(b)
	return out
}

func chicago(sku, _ string, _ map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	return []Candidate{{fmt.Sprintf("https://www.chicagofaucets.com/sites/default/files/spec-sheets/faucets/%s.pdf", url.PathEscape(s)), s + ".pdf"}}
}

func newportBrass(sku, _ string, _ map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	if i := strings.Index(s, "/"); i > 0 {
		s = s[:i]
	}
	return []Candidate{{fmt.Sprintf("https://www.newportbrass.com/documents/dynamicdocuments/spec_%s.pdf", url.PathEscape(s)), "spec_" + s + ".pdf"}}
}

func kohler(sku, _ string, fin map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	b := base(s, fin)
	if b == s {
		b = reFinishTail.ReplaceAllString(s, "") // K-2214-0 → K-2214
		if !strings.HasPrefix(b, "K-") {
			b = s
		}
	}
	return []Candidate{{fmt.Sprintf("https://techcomm.kohler.com/techcomm/pdf/%s_spec_US-CA_Kohler_en.pdf", url.PathEscape(b)), b + "_spec.pdf"}}
}

func kallista(sku, _ string, fin map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	b := base(s, fin)
	if b == s {
		b = reFinishTail.ReplaceAllString(s, "")
	}
	return []Candidate{{fmt.Sprintf("https://techcomm.kohler.com/techcomm/pdf/%s_spec_US-CA_Kallista_en.pdf", url.PathEscape(b)), b + "_spec.pdf"}}
}

func lacava(sku, _ string, fin map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	// Lacava items are the bare model (0201, 5062); options and finishes follow a dot or dash
	// (16D3.T.S, 5452UN): try the full item first, then the bare model
	b := base(s, fin)
	var out []Candidate
	add := func(m string) {
		for _, o := range out {
			if o.Name == m+".pdf" {
				return
			}
		}
		out = append(out, Candidate{fmt.Sprintf("https://lacava.com/pdfs/%s.pdf", url.PathEscape(m)), m + ".pdf"})
	}
	add(b)
	if i := strings.IndexAny(b, "-/."); i > 0 {
		add(b[:i])
	}
	return out
}

func baldwin(sku, _ string, _ map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	if i := strings.Index(s, "."); i > 0 {
		s = s[:i]
	}
	return []Candidate{{fmt.Sprintf("https://baldwinhardware.blob.core.windows.net/documents/%s_techdrawing.pdf", url.PathEscape(s)), s + "_techdrawing.pdf"}}
}

// brizo sheets are BSP-<room letter>-<model> Rev <X>.pdf; the revision is unknowable, so
// a few are tried, the room letter guessed from the category with the others as fallbacks.
func brizo(sku, category string, fin map[string]bool) []Candidate {
	s := strings.ToUpper(strings.TrimSpace(sku))
	b := base(s, fin)
	if b == s {
		// glued finish + handle codes (61023LF-PNLHP): drop a known finish at the start of the tail
		if i := strings.LastIndex(s, "-"); i > 0 {
			tail := s[i+1:]
			for code := range fin {
				if strings.HasPrefix(tail, code) && len(tail) > len(code) {
					b = s[:i+1] + tail[len(code):]
					break
				}
			}
		}
	}
	letters := []string{"B", "K", "L", "A"}
	c := strings.ToLower(category)
	switch {
	case strings.Contains(c, "kitchen") || strings.Contains(c, "bar") || strings.Contains(c, "pot filler") || strings.Contains(c, "filtration"):
		letters = []string{"K", "B", "L", "A"}
	case strings.Contains(c, "sink faucet") || strings.Contains(c, "bidet"):
		letters = []string{"L", "B", "K", "A"}
	case strings.Contains(c, "accessor") || strings.Contains(c, "towel") || strings.Contains(c, "paper"):
		letters = []string{"A", "B", "L", "K"}
	}
	var out []Candidate
	for _, l := range letters[:2] {
		for _, rev := range []string{"A", "B", "C", "D", "E"} {
			name := fmt.Sprintf("BSP-%s-%s Rev %s.pdf", l, b, rev)
			out = append(out, Candidate{"https://media.brizo.com/SpecSheet/" + url.PathEscape(name), name})
		}
	}
	return out
}
