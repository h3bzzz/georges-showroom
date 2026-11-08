package vendors

import (
	"regexp"
	"sort"
	"strings"
)

// California Faucets (verified 2026-11-07): images.calfaucets.com/catalog/_productLarge/
// <MODEL>-<FINISH>.jpg is a 2800 px render of the base model in that finish, for every
// finish the model is offered in; an unknown name is a 403. Price-book SKUs are the site
// model plus the finish code, with a few price-book-only suffixes in between (ZB/ZBF
// ZeroDrain, accent handles -ADC/-BP/…, .18/.20 for the site's .FR).
var calExtraCodes = []string{"MBLK", "BLKN", "MWHT", "ABF", "ANF", "ACF", "BTB", "BBU", "BNU", "PBU", "ORB", "USS", "LPG", "LSG", "LPB", "LSB", "FRG", "GRP", "SBZ", "MOB", "BLK", "WHT", "PC", "SN", "PN", "PB", "SB", "SC", "CB"}

var (
	calSKU    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{2,30}$`)
	calZero   = regexp.MustCompile(`ZBF?(-|$)`)
	calAccent = regexp.MustCompile(`-(ADC|BP|GP|OP|PP|RD|WP|YP)$`)
)

// calSplit separates a price-book SKU into its finish code and the site models it may stand
// for, most specific first. code is "" when the SKU carries no known finish.
func calSplit(sku string, codes map[string]bool) (code string, models []string) {
	s := strings.ToUpper(strings.TrimSpace(sku))
	if !calSKU.MatchString(s) {
		return "", nil
	}
	i := strings.LastIndex(s, "-")
	if i <= 0 || !codes[s[i+1:]] {
		return "", nil
	}
	code, s = s[i+1:], s[:i]
	add := func(m string) {
		m = strings.TrimSuffix(strings.TrimSpace(m), "-")
		if m == "" {
			return
		}
		for _, o := range models {
			if o == m {
				return
			}
		}
		models = append(models, m)
	}
	add(s)
	if z := calZero.ReplaceAllString(s, "$1"); z != s {
		s = z
		add(s)
	}
	if a := calAccent.ReplaceAllString(s, ""); a != s {
		s = a
		add(s)
	}
	for _, m := range append([]string{}, models...) {
		if strings.HasSuffix(m, ".18") || strings.HasSuffix(m, ".20") {
			add(m[:len(m)-3] + ".FR")
		}
	}
	return code, models
}

// calURLs lists the per-finish master candidates for a SKU.
func calURLs(sku string, codes map[string]bool) (code string, urls []string) {
	code, models := calSplit(sku, codes)
	for _, m := range models {
		urls = append(urls, "https://images.calfaucets.com/catalog/_productLarge/"+m+"-"+code+".jpg")
	}
	return code, urls
}

// calCodes merges the brand's finish_codes rows with the codes seen on the site.
func calCodes(db []string) map[string]bool {
	m := map[string]bool{}
	for _, c := range append(append([]string{}, db...), calExtraCodes...) {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			m[c] = true
		}
	}
	return m
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
