// Package webimg finds a product image on a manufacturer's own website for a SKU when the
// catalogs gave us nothing. It tries the site's search page with a few common URL shapes,
// follows the first result that mentions the SKU (or its family key), and takes the page's
// og:image or its largest product image. Everything is rate-limited per domain and cached.
package webimg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

type Result struct {
	PageURL  string
	ImageURL string
	Bytes    []byte
	Ext      string
}

type Client struct {
	http     *http.Client
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	seen     map[string]int // image URL → times returned; a URL that repeats across SKUs is a site default
	braveKey string
}

// New returns a resolver. braveKey (Brave Search API, https://brave.com/search/api/) makes the
// domain-scoped lookup reliable; without it only the site-search heuristics run.
func New(braveKey string) *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}, limiters: map[string]*rate.Limiter{}, seen: map[string]int{}, braveKey: braveKey}
}

func (c *Client) wait(ctx context.Context, domain string) error {
	c.mu.Lock()
	l, ok := c.limiters[domain]
	if !ok {
		l = rate.NewLimiter(rate.Every(1200*time.Millisecond), 1)
		c.limiters[domain] = l
	}
	c.mu.Unlock()
	return l.Wait(ctx)
}

func (c *Client) get(ctx context.Context, u string) ([]byte, string, error) {
	pu, err := url.Parse(u)
	if err != nil {
		return nil, "", err
	}
	if err := c.wait(ctx, pu.Host); err != nil {
		return nil, "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.8")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: %s", u, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return b, res.Header.Get("Content-Type"), err
}

var searchShapes = []string{
	"https://%s/search?q=%s",
	"https://%s/?s=%s",
	"https://%s/search?query=%s",
	"https://%s/catalogsearch/result/?q=%s",
	"https://%s/search/?q=%s",
	"https://%s/products?search=%s",
}

var reImgExt = regexp.MustCompile(`(?i)\.(jpe?g|png|webp)(\?|$)`)

// Resolve tries to find an image for sku (falling back to key) on domain: first via a web
// search restricted to the brand's domain (works for any site), then via the site's own
// search page shapes.
func (c *Client) Resolve(ctx context.Context, domain, sku, key string) (*Result, error) {
	terms := []string{sku}
	if key != "" && !strings.EqualFold(key, sku) {
		terms = append(terms, key)
	}
	for _, term := range terms {
		for _, l := range c.searchEngine(ctx, domain, term) {
			pb, pct, err := c.get(ctx, l)
			if err != nil || !strings.Contains(pct, "html") {
				continue
			}
			pdoc, err := goquery.NewDocumentFromReader(bytes.NewReader(pb))
			if err != nil {
				continue
			}
			if img := c.imageFrom(ctx, pdoc, l, term); img != nil {
				return img, nil
			}
		}
	}
	for _, term := range terms {
		for _, shape := range searchShapes {
			su := fmt.Sprintf(shape, domain, url.QueryEscape(term))
			page, ct, err := c.get(ctx, su)
			if err != nil || !strings.Contains(ct, "html") {
				continue
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
			if err != nil {
				continue
			}
			// the search page itself may be the product page (redirect on exact match)
			if img := c.imageFrom(ctx, doc, su, term); img != nil {
				return img, nil
			}
			var links []string
			doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
				href := a.AttrOr("href", "")
				text := strings.ToLower(a.Text() + " " + href + " " + a.AttrOr("title", ""))
				if strings.Contains(text, strings.ToLower(term)) && !strings.Contains(href, "search") {
					links = append(links, resolve(su, href))
				}
			})
			for i, l := range links {
				if i >= 2 || l == "" {
					break
				}
				pb, pct, err := c.get(ctx, l)
				if err != nil || !strings.Contains(pct, "html") {
					continue
				}
				pdoc, err := goquery.NewDocumentFromReader(bytes.NewReader(pb))
				if err != nil {
					continue
				}
				if img := c.imageFrom(ctx, pdoc, l, term); img != nil {
					return img, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no web image for %s on %s", sku, domain)
}

func (c *Client) imageFrom(ctx context.Context, doc *goquery.Document, pageURL, term string) *Result {
	text := strings.ToLower(doc.Find("title").Text() + " " + doc.Find("h1").Text() + " " + doc.Find("meta[property='og:title']").AttrOr("content", ""))
	if !strings.Contains(text, strings.ToLower(term)) && !strings.Contains(strings.ToLower(doc.Text()), strings.ToLower(term)) {
		return nil
	}
	var cands []string
	lt := strings.ToLower(term)
	// images named after the SKU first, then gallery/product images, then og:image
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		src := firstNonEmpty(s.AttrOr("data-zoom-image", ""), s.AttrOr("data-large_image", ""), s.AttrOr("data-src", ""), s.AttrOr("src", ""))
		meta := strings.ToLower(s.AttrOr("alt", "") + " " + s.AttrOr("class", "") + " " + src)
		if src == "" || strings.Contains(meta, "logo") || strings.Contains(meta, "icon") || strings.Contains(meta, "video") || strings.Contains(meta, "banner") {
			return
		}
		if strings.Contains(strings.ToLower(src), lt) || strings.Contains(strings.ToLower(s.AttrOr("alt", "")), lt) {
			cands = append([]string{src}, cands...)
		} else if strings.Contains(meta, "product") || strings.Contains(meta, "zoom") || strings.Contains(meta, "gallery") {
			cands = append(cands, src)
		}
	})
	if og := doc.Find("meta[property='og:image']").AttrOr("content", ""); og != "" {
		cands = append(cands, og)
	}
	for i, src := range cands {
		if i >= 4 {
			break
		}
		u := resolve(pageURL, src)
		if u == "" || strings.HasPrefix(u, "data:") {
			continue
		}
		c.mu.Lock()
		repeats := c.seen[u]
		c.mu.Unlock()
		if repeats >= 2 {
			continue // same picture for several SKUs: a placeholder, not a product photo
		}
		b, ct, err := c.get(ctx, u)
		if err != nil || len(b) < 3000 {
			continue
		}
		ext := ""
		switch {
		case strings.Contains(ct, "png") || bytes.HasPrefix(b, []byte("\x89PNG")):
			ext = ".png"
		case strings.Contains(ct, "jpeg") || bytes.HasPrefix(b, []byte("\xff\xd8")):
			ext = ".jpg"
		case strings.Contains(ct, "webp") || (len(b) > 12 && string(b[8:12]) == "WEBP"):
			ext = ".webp"
		case reImgExt.MatchString(u):
			ext = strings.ToLower(reImgExt.FindStringSubmatch(u)[0][:4])
		}
		if ext == "" {
			continue
		}
		c.mu.Lock()
		c.seen[u]++
		c.mu.Unlock()
		return &Result{PageURL: pageURL, ImageURL: u, Bytes: b, Ext: ext}
	}
	return nil
}

func resolve(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	r := b.ResolveReference(u)
	if r.Scheme != "http" && r.Scheme != "https" {
		return ""
	}
	return r.String()
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

// searchEngine asks the Brave Search API for pages on the brand's domain mentioning the term
// and returns up to three result URLs on that domain. Returns nil without an API key.
func (c *Client) searchEngine(ctx context.Context, domain, term string) []string {
	if c.braveKey == "" {
		return nil
	}
	if err := c.wait(ctx, "api.search.brave.com"); err != nil {
		return nil
	}
	q := url.QueryEscape("site:" + domain + " " + term)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.search.brave.com/res/v1/web/search?count=5&q="+q, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.braveKey)
	res, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Web struct {
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil
	}
	var out []string
	for _, r := range payload.Web.Results {
		if strings.Contains(r.URL, domain) && len(out) < 3 {
			out = append(out, r.URL)
		}
	}
	return out
}
