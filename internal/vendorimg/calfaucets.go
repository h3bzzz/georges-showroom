package vendorimg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// California Faucets (verified 2026-11-01). The site is Craft CMS with a public JSON catalog:
// /api/v1/catalog?limit=1000&page=N lists every product with its base model, page path and a
// 320 px cover; the cover is a Craft transform whose large sibling (_productLarge, 2800 px on
// the long edge) is one folder swap away. Image file names are whatever the uploader typed
// (single-hole-vessel-bathroom-faucet.jpg for 3501-2), so the key is never derivable from the
// SKU; the catalog is the lookup. Our price-book SKUs are the site model plus a letter finish
// code (3501-2 → 3501-2-SN), with a few price-book-only suffixes (ZB/ZBF ZeroDrain, accent
// handles -ADC/-BP/…, .18/.20 for the site's .FR).
type calProduct struct {
	ID    int64  `json:"id"`
	Model string `json:"model"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Image string `json:"image"`
}

var calFinishCodes = []string{"MBLK", "BLKN", "MWHT", "ABF", "ANF", "ACF", "BTB", "BBU", "BNU", "PBU", "ORB", "USS", "LPG", "LSG", "LPB", "LSB", "FRG", "GRP", "SBZ", "BLK", "WHT", "PC", "SN", "PN", "PB", "SB", "SC", "CB"}

var (
	calSKU    = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{2,30}$`)
	calZero   = regexp.MustCompile(`ZBF?(-|$)`)
	calAccent = regexp.MustCompile(`-(ADC|BP|GP|OP|PP|RD|WP|YP)$`)
	calBar    = regexp.MustCompile(`^(SB|SD)-(\d+)[A-Z]*$`)
	calOnce   sync.Once
	calByMod  map[string]calProduct
	calErr    error
)

// calModels lists the catalog models a price-book SKU may stand for, most specific first.
func calModels(sku string) []string {
	var out []string
	add := func(m string) {
		m = strings.TrimSuffix(strings.TrimSpace(m), "-")
		if m == "" {
			return
		}
		for _, o := range out {
			if o == m {
				return
			}
		}
		out = append(out, m)
	}
	s := strings.ToUpper(sku)
	add(s)
	for _, f := range calFinishCodes {
		if strings.HasSuffix(s, "-"+f) {
			s = strings.TrimSuffix(s, "-"+f)
			add(s)
			break
		}
	}
	if z := calZero.ReplaceAllString(s, "$1"); z != s {
		s = z
		add(s)
	}
	if a := calAccent.ReplaceAllString(s, ""); a != s {
		add(a)
		s = a
	}
	for _, m := range append([]string{}, out...) {
		if strings.HasSuffix(m, ".18") || strings.HasSuffix(m, ".20") {
			add(m[:len(m)-3] + ".FR")
		}
		if b := calBar.FindStringSubmatch(m); b != nil {
			add(b[1] + "-" + b[2] + "-XX")
		}
	}
	return out
}

// calCatalog loads the site catalog once per process, from a day-old cache file when present.
func calCatalog(ctx context.Context, c *Client, dir string) (map[string]calProduct, error) {
	calOnce.Do(func() {
		cache := filepath.Join(dir, "CAL", "_catalog.json")
		var rows []calProduct
		if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
			if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &rows) == nil && len(rows) > 100 {
				calByMod = calIndex(rows)
				return
			}
		}
		for page := 1; page <= 20; page++ {
			u := fmt.Sprintf("https://www.calfaucets.com/api/v1/catalog?limit=1000&page=%d", page)
			body, status, err := c.get(ctx, u, 8<<20)
			if err != nil {
				calErr = err
				return
			}
			if status != http.StatusOK {
				calErr = fmt.Errorf("GET %s: %d", u, status)
				return
			}
			var env struct {
				Data []calProduct `json:"data"`
			}
			if err := json.Unmarshal(body, &env); err != nil {
				calErr = fmt.Errorf("%s: %w", u, err)
				return
			}
			if len(env.Data) == 0 {
				break
			}
			rows = append(rows, env.Data...)
			if len(env.Data) < 1000 {
				break
			}
		}
		if len(rows) == 0 {
			calErr = fmt.Errorf("calfaucets catalog: no rows")
			return
		}
		if b, err := json.Marshal(rows); err == nil {
			_ = os.MkdirAll(filepath.Dir(cache), 0o755)
			_ = os.WriteFile(cache, b, 0o644)
		}
		calByMod = calIndex(rows)
	})
	return calByMod, calErr
}

func calIndex(rows []calProduct) map[string]calProduct {
	m := make(map[string]calProduct, len(rows))
	for _, r := range rows {
		k := strings.ToUpper(strings.TrimSpace(r.Model))
		if k == "" {
			continue
		}
		if cur, ok := m[k]; !ok || (cur.Image == "" && r.Image != "") {
			m[k] = r
		}
	}
	return m
}

// calResolve maps a SKU to the product's large cover: the catalog thumb transform swapped for
// _productLarge, then the other asset folder, then the untransformed original.
func calResolve(ctx context.Context, c *Client, sku string) (string, error) {
	if !calSKU.MatchString(sku) {
		return "", ErrNoScheme
	}
	cat, err := calCatalog(ctx, c, c.dir)
	if err != nil {
		return "", err
	}
	var p calProduct
	found := false
	for _, m := range calModels(sku) {
		if p, found = cat[m]; found {
			break
		}
	}
	if !found || p.Image == "" {
		return "", nil
	}
	large := strings.Replace(p.Image, "_productThumb", "_productLarge", 1)
	if large == p.Image {
		return "", nil
	}
	var tries []string
	tries = append(tries, large)
	if strings.Contains(large, "/catalog/") {
		tries = append(tries, strings.Replace(large, "/catalog/", "/covers/", 1))
	} else if strings.Contains(large, "/covers/") {
		tries = append(tries, strings.Replace(large, "/covers/", "/catalog/", 1))
	}
	tries = append(tries, strings.Replace(large, "_productLarge/", "", 1))
	for _, u := range tries {
		if err := c.limiter.Wait(ctx); err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (georges-showroom dealer sync)")
		res, err := c.http.Do(req)
		if err != nil {
			return "", err
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK && strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
			return u, nil
		}
	}
	return "", nil
}
