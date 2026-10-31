package site

import (
	"io"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Listing is a category / brand / search results page (24 product families per page).
type Listing struct {
	URL        string        `json:"url"`
	Title      string        `json:"title"` // "Search info: Maidstone Tubs"
	Page       int           `json:"page"`
	TotalPages int           `json:"total_pages"`
	NextURL    string        `json:"next_url,omitempty"`
	PrevURL    string        `json:"prev_url,omitempty"`
	Selections []Selection   `json:"selections,omitempty"`
	Facets     []Facet       `json:"facets,omitempty"`
	Sorts      []Link        `json:"sorts,omitempty"`
	Items      []ListingItem `json:"items"`
}

type Selection struct {
	Label     string `json:"label"`      // "Brand: Maidstone"
	RemoveURL string `json:"remove_url"` // URL without this filter
}

type Facet struct {
	Name   string       `json:"name"` // Category, Color, Collection, Brand, Type, Specials
	Values []FacetValue `json:"values"`
}

type FacetValue struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	URL   string `json:"url"`
}

// ListingItem is one product-family card.
type ListingItem struct {
	URL           string `json:"url"`
	Brand         string `json:"brand"`
	BrandURL      string `json:"brand_url,omitempty"`
	Title         string `json:"title"`
	SKU           string `json:"sku"`
	Image         string `json:"image,omitempty"`
	PriceMinCents int64  `json:"price_min_cents,omitempty"`
	PriceMaxCents int64  `json:"price_max_cents,omitempty"`
	FinishCount   int    `json:"finish_count,omitempty"`
	FamilyURL     string `json:"family_url,omitempty"` // ?model=… link listing all finishes
	FamilyKey     string `json:"family_key,omitempty"`
	Category      string `json:"category,omitempty"`
	CategoryURL   string `json:"category_url,omitempty"`
}

// ParseListing parses a listing page.
func ParseListing(r io.Reader, pageURL string) (*Listing, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	l := &Listing{URL: pageURL}
	if c := FixURL(attrOr(doc.Find(`link[rel="canonical"]`), "href")); c != "" {
		l.URL = c
	}
	l.Title = strings.TrimSpace(strings.TrimPrefix(clean(doc.Find("#top-header h1").First().Text()), "Search info:"))

	if m := rePageOf.FindStringSubmatch(clean(doc.Find("ul.pagination").First().Text())); m != nil {
		l.Page, _ = strconv.Atoi(m[1])
		l.TotalPages, _ = strconv.Atoi(m[2])
	}
	l.NextURL = abs(attrOr(doc.Find("a.page-next[href]").First(), "href"))
	l.PrevURL = abs(attrOr(doc.Find("a.page-prev[href]").First(), "href"))

	seenSel := map[string]bool{}
	doc.Find("ul.fitler-list li.list-inline-item").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a[href]").First()
		if a.Length() == 0 {
			return
		}
		label := clean(strings.Replace(clean(li.Text()), "Remove this item", "", 1))
		if label == "" || label == "Clear All" || seenSel[label] {
			return
		}
		seenSel[label] = true
		l.Selections = append(l.Selections, Selection{Label: label, RemoveURL: abs(attrOr(a, "href"))})
	})

	doc.Find(".wrap-dropdown-content").Each(func(_ int, w *goquery.Selection) {
		f := Facet{Name: clean(w.Find("p.h6").First().Text())}
		w.Find(".dropdown-content a[href]").Each(func(_ int, a *goquery.Selection) {
			label, n := labeledCount(a.Text())
			f.Values = append(f.Values, FacetValue{Label: label, Count: n, URL: abs(attrOr(a, "href"))})
		})
		if f.Name != "" && len(f.Values) > 0 {
			l.Facets = append(l.Facets, f)
		}
	})

	doc.Find("a.dropdown-item[href*='sort=']").Each(func(_ int, a *goquery.Selection) {
		l.Sorts = append(l.Sorts, Link{Text: clean(a.Text()), URL: abs(attrOr(a, "href"))})
	})

	doc.Find(".row.products .product").Each(func(_ int, c *goquery.Selection) {
		a := c.Find("a[href*='/Pasadena-California/']").First()
		if a.Length() == 0 {
			return
		}
		it := ListingItem{URL: abs(attrOr(a, "href")), Title: attrOr(a, "title")}
		it.Image = abs(attrOr(c.Find(".image img").First(), "src"))
		if b := c.Find("p.title a").First(); b.Length() > 0 {
			it.Brand = clean(b.Text())
			it.BrandURL = abs(attrOr(b, "href"))
		}
		if d := clean(c.Find("p.description").First().Text()); d != "" {
			it.Title = d
		}
		it.SKU = clean(c.Find("small").First().Text())
		prices := moneyAll(c.Find("p.price").First().Text())
		if len(prices) > 0 {
			it.PriceMinCents = prices[0]
			it.PriceMaxCents = prices[len(prices)-1]
		}
		c.Find("ul.product-text li a[href]").Each(func(_ int, x *goquery.Selection) {
			href := attrOr(x, "href")
			if strings.Contains(href, "model=") {
				it.FamilyURL = abs(href)
				if m := reModel.FindStringSubmatch(href); m != nil {
					it.FamilyKey = strings.ToUpper(m[1])
				}
				if m := reFinishCount.FindStringSubmatch(clean(x.Text())); m != nil {
					it.FinishCount, _ = strconv.Atoi(m[1])
				}
			} else if it.Category == "" {
				it.Category = clean(x.Text())
				it.CategoryURL = abs(href)
			}
		})
		l.Items = append(l.Items, it)
	})
	if len(l.Items) == 0 && l.TotalPages == 0 {
		return l, ErrNotListing
	}
	return l, nil
}

// IsChallenge reports whether an HTML document is a Cloudflare / iAmHuman interstitial.
func IsChallenge(html string) bool {
	return strings.Contains(html, "Just a moment...") || strings.Contains(html, "iAmHuman") && !strings.Contains(html, "product-detail-name") && !strings.Contains(html, "row products")
}
