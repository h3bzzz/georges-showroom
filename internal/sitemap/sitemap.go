// Package sitemap reads the site's sitemap index (zgeorges-SiteIndex.xml → SiteIndexN.xml).
// The XML must be fetched through the browser session because of Cloudflare; this package
// only parses.
package sitemap

import (
	"encoding/xml"
	"regexp"
	"strings"
)

type urlset struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

type sitemapindex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

var reLoc = regexp.MustCompile(`https://www\.georgesshowroom\.com/[^<\s"']+`)

// Locs extracts every <loc> from either a sitemap index or a urlset. It falls back to a regex
// scan so that a browser-rendered copy of the XML (innerText) still works.
func Locs(doc string) []string {
	doc = strings.TrimSpace(doc)
	var idx sitemapindex
	if err := xml.Unmarshal([]byte(doc), &idx); err == nil && len(idx.Sitemaps) > 0 {
		out := make([]string, 0, len(idx.Sitemaps))
		for _, s := range idx.Sitemaps {
			out = append(out, strings.TrimSpace(s.Loc))
		}
		return out
	}
	var us urlset
	if err := xml.Unmarshal([]byte(doc), &us); err == nil && len(us.URLs) > 0 {
		out := make([]string, 0, len(us.URLs))
		for _, u := range us.URLs {
			out = append(out, strings.TrimSpace(u.Loc))
		}
		return out
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range reLoc.FindAllString(doc, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// Kind classifies a site URL.
type Kind string

const (
	KindProduct  Kind = "product"
	KindImage    Kind = "image"
	KindBrand    Kind = "brand"
	KindCategory Kind = "category"
	KindOther    Kind = "other"
)

var (
	reImage    = regexp.MustCompile(`(?i)\.(jpe?g|png|gif)$`)
	reBrand    = regexp.MustCompile(`(?i)-v1\.htm$`)
	reCategory = regexp.MustCompile(`(?i)-v\d+h?\.htm$`)
)

func Classify(u string) Kind {
	switch {
	case strings.Contains(u, "/Pasadena-California/"):
		return KindProduct
	case reImage.MatchString(u):
		return KindImage
	case reBrand.MatchString(u):
		return KindBrand
	case reCategory.MatchString(u):
		return KindCategory
	}
	return KindOther
}
