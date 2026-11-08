package vendors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Crawler builds the SKU → image URL maps that the map-based sources (Emtek, Chicago Faucets)
// read: manufacturers whose picture names cannot be derived from a SKU but whose sites list
// them per product. Maps live in DATA_DIR/webimg/<CODE>/ and are merged on every run.
type Crawler struct {
	DataDir string
	Brand   string
	Limit   int
	Client  *http.Client
	lim     *rate.Limiter
}

// ImageRow is one picture the crawl found.
type ImageRow struct {
	SKU        string `json:"sku"`
	Model      string `json:"model,omitempty"`
	FinishCode string `json:"finish_code,omitempty"`
	FinishName string `json:"finish_name,omitempty"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url"`
}

// Run crawls one brand and returns rows written.
func (c *Crawler) Run(ctx context.Context) (int, error) {
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 45 * time.Second}
	}
	c.lim = rate.NewLimiter(rate.Every(300*time.Millisecond), 1)
	switch c.Brand {
	case "ETK":
		return c.emtek(ctx)
	case "CHI":
		return c.chicago(ctx)
	case "SGM":
		return c.sigma(ctx)
	}
	return 0, fmt.Errorf("no crawler for %s (have ETK, CHI, SGM)", c.Brand)
}

func (c *Crawler) get(ctx context.Context, u string) ([]byte, int, error) {
	if err := c.lim.Wait(ctx); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) GeorgesShowroom/2 (+https://georgesshowroom.com)")
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.5")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, resp.StatusCode, err
}

// mergeMap reads the existing map, adds rows (by URL), writes it back, returns the row count.
func (c *Crawler) mergeMap(file string, rows []ImageRow) (int, error) {
	p := filepath.Join(c.DataDir, "webimg", c.Brand, file)
	var have []ImageRow
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &have)
	}
	seen := map[string]bool{}
	var out []ImageRow
	for _, r := range append(have, rows...) {
		if r.URL == "" || r.SKU == "" || seen[r.URL+"|"+r.SKU] {
			continue
		}
		seen[r.URL+"|"+r.SKU] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKU < out[j].SKU })
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	b, _ := json.MarshalIndent(out, "", " ")
	return len(out), os.WriteFile(p, b, 0o644)
}

// ---- Emtek: Wagtail pages API ------------------------------------------------------------
// /api/v2/pages/?type=cms.ProductPage lists every product (20 per call); each page's
// attribute.finish_images maps a finish id to a Salsify picture whose file name carries the
// full SKU ("…-4914US15.LS15.KN15_EM_KO_3Q-EXT.jpg"): the SKU, its model (leading digits) and
// the finish code (US15, MB, FB …) are read from that name.

var etkFile = regexp.MustCompile(`-(\d{4,6}[A-Z]?[A-Z0-9]*(?:\.[A-Z0-9]+)*)_EM_`)

func (c *Crawler) emtek(ctx context.Context) (int, error) {
	// phase 1: product page ids (the list endpoint cannot return attribute fields)
	var ids []int
	titles := map[int]string{}
	total := 0
	for offset := 0; ; offset += 20 {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		body, status, err := c.get(ctx, fmt.Sprintf("https://www.emtek.com/api/v2/pages/?type=cms.ProductPage&limit=20&offset=%d", offset))
		if err != nil {
			return 0, err
		}
		if status != 200 {
			return 0, fmt.Errorf("emtek api: HTTP %d at offset %d", status, offset)
		}
		var d struct {
			Meta struct {
				TotalCount int `json:"total_count"`
			} `json:"meta"`
			Items []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			return 0, fmt.Errorf("emtek api: %w", err)
		}
		total = d.Meta.TotalCount
		for _, it := range d.Items {
			ids = append(ids, it.ID)
			titles[it.ID] = it.Title
		}
		if len(d.Items) < 20 || offset+20 >= total || (c.Limit > 0 && len(ids) >= c.Limit) {
			break
		}
	}
	slog.Info("emtek crawl", "product_pages", len(ids), "of", total)
	// phase 2: each page's finish pictures
	var rows []ImageRow
	for i, id := range ids {
		if ctx.Err() != nil {
			break
		}
		if c.Limit > 0 && i >= c.Limit {
			break
		}
		body, status, err := c.get(ctx, fmt.Sprintf("https://www.emtek.com/api/v2/pages/%d/", id))
		if err != nil || status != 200 {
			slog.Warn("emtek crawl: page", "id", id, "status", status, "err", err)
			continue
		}
		var d struct {
			Attribute struct {
				Code         string            `json:"code"`
				FinishImages map[string]string `json:"finish_images"`
			} `json:"attribute"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			continue
		}
		for _, u := range d.Attribute.FinishImages {
			if u == "" {
				continue
			}
			m := etkFile.FindStringSubmatch(baseName(u))
			if m == nil {
				continue
			}
			sku := m[1]
			row := ImageRow{SKU: sku, URL: u, Title: titles[id]}
			if n := etkModel.FindStringSubmatch(sku); n != nil {
				row.Model = n[1]
			}
			head := sku
			if j := strings.IndexByte(head, '.'); j > 0 {
				head = head[:j]
			}
			if f := etkCode.FindStringSubmatch(head); f != nil {
				row.FinishCode = f[1]
			}
			rows = append(rows, row)
		}
		if (i+1)%100 == 0 {
			slog.Info("emtek crawl", "pages", i+1, "of", len(ids), "pictures", len(rows))
			if _, err := c.mergeMap("_finish_images.json", rows); err != nil {
				return 0, err
			}
			rows = rows[:0]
		}
	}
	n, err := c.mergeMap("_finish_images.json", rows)
	slog.Info("emtek crawl done", "pages", len(ids), "map_rows", n)
	return n, err
}

// ---- Chicago Faucets: sitemap → product page → component picture -------------------------
// sitemap.xml?page=N lists /products/<group>/<line>/<SKU>; the product page links the full
// picture at /sites/default/files/components/<name>.jpg (the name is the component's, not
// always the SKU's, so the page has to be read).

var (
	chiLoc  = regexp.MustCompile(`<loc>(https://www\.chicagofaucets\.com/products/[^<]+)</loc>`)
	chiImg  = regexp.MustCompile(`href="(/sites/default/files/components/[^"]+\.(?:jpg|jpeg|png))"`)
	chiImg2 = regexp.MustCompile(`src="(/sites/default/files/styles/product_default/public/components/[^"?]+\.(?:jpg|jpeg|png))`)
)

func (c *Crawler) chicago(ctx context.Context) (int, error) {
	// product URLs from the sitemap index
	var urls []string
	for p := 1; p <= 60; p++ {
		body, status, err := c.get(ctx, fmt.Sprintf("https://www.chicagofaucets.com/sitemap.xml?page=%d", p))
		if err != nil {
			return 0, err
		}
		if status != 200 {
			break
		}
		found := 0
		for _, m := range chiLoc.FindAllStringSubmatch(string(body), -1) {
			urls = append(urls, m[1])
			found++
		}
		if found == 0 && !strings.Contains(string(body), "<url>") {
			break
		}
	}
	slog.Info("chicago crawl", "product_urls", len(urls))
	// what the map already knows, so a rerun only visits new SKUs
	known := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(c.DataDir, "webimg", c.Brand, "_images.json")); err == nil {
		var have []ImageRow
		_ = json.Unmarshal(b, &have)
		for _, r := range have {
			known[strings.ToUpper(r.SKU)] = true
		}
	}
	var rows []ImageRow
	visited, misses := 0, 0
	for _, u := range urls {
		if ctx.Err() != nil {
			break
		}
		sku := strings.ToUpper(baseName(u))
		if known[sku] {
			continue
		}
		body, status, err := c.get(ctx, u)
		visited++
		if err != nil || status != 200 {
			misses++
			continue
		}
		img := ""
		if m := chiImg.FindStringSubmatch(string(body)); m != nil {
			img = m[1]
		} else if m := chiImg2.FindStringSubmatch(string(body)); m != nil {
			img = strings.Replace(m[1], "/styles/product_default/public", "", 1)
		}
		if img == "" {
			misses++
			continue
		}
		rows = append(rows, ImageRow{SKU: sku, URL: "https://www.chicagofaucets.com" + img})
		if visited%200 == 0 {
			slog.Info("chicago crawl", "visited", visited, "pictures", len(rows), "misses", misses)
			if _, err := c.mergeMap("_images.json", rows); err != nil {
				return 0, err
			}
			rows = rows[:0]
		}
		if c.Limit > 0 && visited >= c.Limit {
			break
		}
	}
	n, err := c.mergeMap("_images.json", rows)
	slog.Info("chicago crawl done", "visited", visited, "misses", misses, "map_rows", n)
	return n, err
}

// ---- Sigma Designs: Magento sitemap → product page (itemprop sku, og:image) ---------------
// sitemap.xml lists /catalog/product/view/id/<n>; the page prints the maker's SKU
// (itemprop="sku", "1.22SP00") and an 800 px og:image under /pub/media/catalog/product/.

var (
	sgmLoc   = regexp.MustCompile(`<loc>(https?://www\.sigmafaucet\.com/catalog/product/view/id/\d+[^<]*)</loc>`)
	sgmSKU   = regexp.MustCompile(`itemprop="sku"[^>]*>\s*([^<\s]+)`)
	sgmImg   = regexp.MustCompile(`og:image" content="([^"]+)"`)
	sgmCache = regexp.MustCompile(`/cache/[0-9a-f]+/`)
)

func (c *Crawler) sigma(ctx context.Context) (int, error) {
	body, status, err := c.get(ctx, "https://www.sigmafaucet.com/sitemap.xml")
	if err != nil {
		return 0, err
	}
	if status != 200 {
		return 0, fmt.Errorf("sigma sitemap: HTTP %d", status)
	}
	var urls []string
	for _, m := range sgmLoc.FindAllStringSubmatch(string(body), -1) {
		urls = append(urls, strings.Replace(m[1], "http://", "https://", 1))
	}
	slog.Info("sigma crawl", "product_urls", len(urls))
	known := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(c.DataDir, "webimg", c.Brand, "_images.json")); err == nil {
		var have []ImageRow
		_ = json.Unmarshal(b, &have)
		for _, r := range have {
			known[r.URL] = true
		}
	}
	var rows []ImageRow
	visited, misses := 0, 0
	for _, u := range urls {
		if ctx.Err() != nil {
			break
		}
		if known["page:"+u] {
			continue
		}
		body, status, err := c.get(ctx, u)
		visited++
		if err != nil || status != 200 {
			misses++
			continue
		}
		sm := sgmSKU.FindStringSubmatch(string(body))
		im := sgmImg.FindStringSubmatch(string(body))
		if sm == nil || im == nil {
			misses++
			continue
		}
		img := sgmCache.ReplaceAllString(im[1], "/") // the uncached original
		rows = append(rows, ImageRow{SKU: strings.ToUpper(strings.TrimSpace(sm[1])), URL: img, Title: u})
		if visited%200 == 0 {
			slog.Info("sigma crawl", "visited", visited, "pictures", len(rows), "misses", misses)
			if _, err := c.mergeMap("_images.json", rows); err != nil {
				return 0, err
			}
			rows = rows[:0]
		}
		if c.Limit > 0 && visited >= c.Limit {
			break
		}
	}
	n, err := c.mergeMap("_images.json", rows)
	slog.Info("sigma crawl done", "visited", visited, "misses", misses, "map_rows", n)
	return n, err
}
