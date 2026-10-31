package site

import (
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Product is one SKU page.
type Product struct {
	URL             string            `json:"url"`
	OnlineProductID int64             `json:"online_product_id"`
	Brand           string            `json:"brand"`
	BrandURL        string            `json:"brand_url"`
	SKU             string            `json:"sku"`
	MPN             string            `json:"mpn,omitempty"`
	UPC             string            `json:"upc,omitempty"`
	Name            string            `json:"name"`         // h1 text without the brand
	Description     string            `json:"description"`  // meta itemprop=description / details paragraph
	ProductType     string            `json:"product_type"` // "Drop In Bathroom Sink"
	Finish          string            `json:"finish"`
	Collection      string            `json:"collection,omitempty"`
	CollectionURL   string            `json:"collection_url,omitempty"`
	FamilyKey       string            `json:"family_key,omitempty"` // from ?model= links or finish options
	PriceCents      int64             `json:"price_cents"`
	MSRPCents       int64             `json:"msrp_cents,omitempty"`
	SavePct         int               `json:"save_pct,omitempty"`
	Currency        string            `json:"currency"`
	Breadcrumbs     []Link            `json:"breadcrumbs"`
	Images          []string          `json:"images"`
	LocalImages     map[string]string `json:"local_images,omitempty"` // url → path relative to DATA_DIR
	Attributes      map[string]string `json:"attributes"`             // Length, Width, Height, Weight, Mounting, ADA, Material, Country of Origin, Shipping…
	Bullets         []string          `json:"bullets,omitempty"`
	Documents       []Document        `json:"documents,omitempty"`
	FinishOptions   []FinishOption    `json:"finish_options,omitempty"`
	FinishCount     int               `json:"finish_count,omitempty"`
	CollectionItems []Card            `json:"collection_items,omitempty"`
	Related         []Card            `json:"related,omitempty"`
	Catalogs        []Link            `json:"catalogs,omitempty"`
	QuoteID         string            `json:"quote_id,omitempty"` // AOQ
}

type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type Document struct {
	Kind string `json:"kind"` // spec | install | other
	URL  string `json:"url"`
	Text string `json:"text"`
}

type FinishOption struct {
	SKU        string `json:"sku"`
	Finish     string `json:"finish"`
	PriceCents int64  `json:"price_cents"`
	URL        string `json:"url"`
	Swatch     string `json:"swatch,omitempty"`
}

// Card is a compact product tile used in collection/related/listing grids.
type Card struct {
	URL      string `json:"url"`
	SKU      string `json:"sku,omitempty"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	Image    string `json:"image,omitempty"`
}

var (
	reFinishOpt   = regexp.MustCompile(`^(.*?)\s+-\s+(.*?)\s+-\s+\$`)
	reFinishCount = regexp.MustCompile(`(\d+)\s+Finish Options`)
	reAOQ         = regexp.MustCompile(`AOQ=(\d+)`)
	reModel       = regexp.MustCompile(`[?&]model=([^&]+)`)
)

// ParseProduct parses a product page. pageURL is used when the canonical link is missing.
func ParseProduct(r io.Reader, pageURL string) (*Product, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	p := &Product{URL: pageURL, Currency: "USD", Attributes: map[string]string{}}
	if c := FixURL(attrOr(doc.Find(`link[rel="canonical"]`), "href")); c != "" {
		p.URL = c
	}

	// schema.org microdata (most reliable)
	meta := func(prop string) string {
		return attrOr(doc.Find(`[itemprop="`+prop+`"]`).First(), "content")
	}
	p.Name = clean(meta("name"))
	p.SKU = meta("sku")
	p.MPN = meta("mpn")
	p.Brand = clean(meta("brand"))
	if p.Brand == "" {
		p.Brand = clean(meta("manufacturer"))
	}
	p.Description = clean(meta("description"))
	if g := meta("gtin13"); g != "" {
		p.UPC = strings.TrimLeft(g, "0")
	}
	if pr := meta("price"); pr != "" {
		if f, err := strconv.ParseFloat(pr, 64); err == nil {
			p.PriceCents = int64(f*100 + 0.5)
		}
	}
	if cur := meta("priceCurrency"); cur != "" {
		p.Currency = cur
	}

	// header fallbacks
	h1 := doc.Find("h1.product-detail-name").First()
	if p.Brand == "" {
		p.Brand = clean(h1.Find("strong").First().Text())
	}
	if p.Name == "" {
		p.Name = clean(strings.TrimPrefix(clean(h1.Text()), p.Brand))
	}
	if p.SKU == "" {
		p.SKU = strings.TrimSpace(strings.TrimPrefix(clean(doc.Find("p.sku-text").First().Text()), "SKU:"))
	}
	if p.PriceCents == 0 {
		p.PriceCents, _ = money(doc.Find(".price-2 h2").First().Text())
	}
	doc.Find("p").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t := clean(s.Text())
		if strings.HasPrefix(t, "Originally") {
			p.MSRPCents, _ = money(t)
			if m := rePctSave.FindStringSubmatch(t); m != nil {
				p.SavePct, _ = strconv.Atoi(m[1])
			}
			return false
		}
		return true
	})

	// crumbs: brand › category › subcategory
	doc.Find("a.crumb").Each(func(i int, s *goquery.Selection) {
		l := Link{Text: clean(s.Text()), URL: abs(attrOr(s, "href"))}
		p.Breadcrumbs = append(p.Breadcrumbs, l)
		if i == 0 && p.BrandURL == "" {
			p.BrandURL = l.URL
		}
	})

	// images
	seen := map[string]bool{}
	addImg := func(src string) {
		u := abs(src)
		if u != "" && !seen[u] && !strings.HasSuffix(u, "/BLA.JPEg") {
			seen[u] = true
			p.Images = append(p.Images, u)
		}
	}
	if im := meta("image"); im != "" {
		addImg(im)
	}
	doc.Find("#slider img, .exzoom_img_ul img, #magiczoom img").Each(func(_ int, s *goquery.Selection) { addImg(attrOr(s, "src")) })
	doc.Find("#main-magiczoom").Each(func(_ int, s *goquery.Selection) { addImg(attrOr(s, "href")) })

	// quick facts under the price ("Product Type", dims)
	doc.Find(".left-details-column ul li").Each(func(_ int, li *goquery.Selection) {
		li.Find("strong").Each(func(_ int, st *goquery.Selection) {
			key := clean(st.Text())
			val := clean(nextText(st))
			if key == "Product Type" {
				p.ProductType = val
			} else if key != "" && val != "" {
				if _, ok := p.Attributes[key]; !ok {
					p.Attributes[key] = val
				}
			}
		})
	})

	// #details: "Brand: SKU" header + <li><strong>Key:</strong> value</li>
	details := doc.Find("#details")
	if p.Description == "" {
		p.Description = clean(details.Find(".details-block > p").First().Text())
	}
	details.Find("li").Each(func(_ int, li *goquery.Selection) {
		st := li.Find("strong").First()
		key := strings.TrimSuffix(clean(st.Text()), ":")
		val := clean(strings.TrimSpace(strings.TrimPrefix(clean(li.Text()), clean(st.Text()))))
		if key == "" || val == "" {
			return
		}
		switch key {
		case "Finish":
			p.Finish = val
		case "Collection":
			p.Collection = val
			p.CollectionURL = abs(attrOr(li.Find("a").First(), "href"))
		case "UPC":
			if p.UPC == "" {
				p.UPC = val
			}
		case "Online Product ID":
			p.OnlineProductID, _ = strconv.ParseInt(val, 10, 64)
		default:
			p.Attributes[key] = val
		}
	})
	if p.Finish == "" {
		doc.Find("li small strong").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if clean(s.Text()) == "Finish:" {
				p.Finish = clean(nextText(s))
				return false
			}
			return true
		})
	}
	doc.Find("#additional-info li").Each(func(_ int, li *goquery.Selection) {
		if t := clean(li.Text()); t != "" {
			p.Bullets = append(p.Bullets, t)
		}
	})

	// finish options dropdown: "NSL3619-AX - Ash - $1,123.50"
	doc.Find(".dropdown-menu a.dropdown-item[href*='/Pasadena-California/']").Each(func(_ int, a *goquery.Selection) {
		t := clean(a.Text())
		fo := FinishOption{URL: abs(attrOr(a, "href")), Swatch: abs(attrOr(a.Find("img"), "src"))}
		if m := reFinishOpt.FindStringSubmatch(t); m != nil {
			fo.SKU, fo.Finish = m[1], m[2]
		}
		fo.PriceCents, _ = money(t)
		p.FinishOptions = append(p.FinishOptions, fo)
	})
	if m := reFinishCount.FindStringSubmatch(doc.Text()); m != nil {
		p.FinishCount, _ = strconv.Atoi(m[1])
	}
	if p.FinishCount == 0 {
		p.FinishCount = len(p.FinishOptions)
	}

	// documents (spec/install sheets on documents.cx1accel.com) and vendor catalogs
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := attrOr(a, "href")
		text := clean(a.Text())
		switch {
		case strings.Contains(href, "cx1accel.com"), strings.HasSuffix(strings.ToLower(href), ".pdf"):
			kind := "other"
			lh := strings.ToLower(href + " " + text)
			if strings.Contains(lh, "spec") {
				kind = "spec"
			} else if strings.Contains(lh, "install") {
				kind = "install"
			}
			p.Documents = append(p.Documents, Document{Kind: kind, URL: abs(href), Text: text})
		case strings.Contains(href, "catalogs.myplumbingshowroom.com"):
			p.Catalogs = append(p.Catalogs, Link{Text: text, URL: abs(href)})
		case p.QuoteID == "" && strings.Contains(href, "AOQ="):
			if m := reAOQ.FindStringSubmatch(href); m != nil {
				p.QuoteID = m[1]
			}
		case p.FamilyKey == "" && strings.Contains(href, "model="):
			if m := reModel.FindStringSubmatch(href); m != nil {
				p.FamilyKey = strings.ToUpper(m[1])
			}
		}
	})

	p.CollectionItems = parseCards(doc.Find("#collection .card"))
	p.Related = parseCards(doc.Find("#relatedProducts .card"))

	if p.OnlineProductID == 0 {
		if v := attrOr(doc.Find(`input[name="PVSN"]`).First(), "value"); v != "" {
			p.OnlineProductID, _ = strconv.ParseInt(strings.TrimRight(v, "SW"), 10, 64)
		}
	}
	if p.SKU == "" {
		return p, ErrNotProduct
	}
	return p, nil
}

// parseCards reads the small product tiles used in collection / related sections.
func parseCards(sel *goquery.Selection) []Card {
	var out []Card
	sel.Each(func(_ int, c *goquery.Selection) {
		a := c.Find("a[href*='/Pasadena-California/']").First()
		if a.Length() == 0 {
			return
		}
		card := Card{URL: abs(attrOr(a, "href")), Image: abs(attrOr(c.Find("img").First(), "src"))}
		body := c.Find(".card-body a").First()
		card.SKU = clean(body.Find("strong").First().Text())
		card.Category = clean(strings.TrimPrefix(clean(body.Text()), card.SKU))
		card.Title = attrOr(c.Find("img").First(), "title")
		out = append(out, card)
	})
	return out
}

// nextText returns the text immediately following a node (its next sibling text/inline nodes)
// up to the next <strong>.
func nextText(s *goquery.Selection) string {
	var b strings.Builder
	for n := s.Nodes[0].NextSibling; n != nil; n = n.NextSibling {
		if n.Data == "strong" {
			break
		}
		b.WriteString(goquery.NewDocumentFromNode(n).Text())
	}
	return b.String()
}
