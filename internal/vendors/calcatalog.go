package vendors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// The site is Craft CMS with a public JSON catalog: /api/v1/catalog?limit=1000&page=N lists
// every product with its model, page path and a 320 px cover. The cover file name is
// whatever the uploader typed (6502-SN.jpg, TO-THRR1-30F_vert-SN.jpg, K51-100SQ-FB-SN.jpg),
// so the finish code inside it is swapped for the SKU's code and the _productLarge
// transform (2800 px) is asked for.
type calProduct struct {
	Model string `json:"model"`
	Path  string `json:"path"`
	Image string `json:"image"`
}

// calCatalog loads the site catalog, from a week-old cache file when present.
func calCatalog(ctx context.Context, c *http.Client, dir string) (map[string]calProduct, error) {
	cache := filepath.Join(dir, "webimg", "CAL", "_catalog.json")
	var rows []calProduct
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < 7*24*time.Hour {
		if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &rows) == nil && len(rows) > 100 {
			return calIndex(rows), nil
		}
	}
	for page := 1; page <= 20; page++ {
		u := fmt.Sprintf("https://www.calfaucets.com/api/v1/catalog?limit=1000&page=%d", page)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", "GeorgesShowroom/2 (dealer image sync; +https://georgesshowroom.com)")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %d", u, resp.StatusCode)
		}
		var env struct {
			Data []calProduct `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("%s: %w", u, err)
		}
		rows = append(rows, env.Data...)
		if len(env.Data) < 1000 {
			break
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("calfaucets catalog: no rows")
	}
	if b, err := json.Marshal(rows); err == nil {
		_ = os.MkdirAll(filepath.Dir(cache), 0o755)
		_ = os.WriteFile(cache, b, 0o644)
	}
	return calIndex(rows), nil
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

// calCoverSwap rewrites the site's cover file name for another finish code: the first
// token that is a finish code is replaced. "" when the name carries no code.
func calCoverSwap(cover, code string, codes map[string]bool) string {
	name := path.Base(cover)
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// tokens keep their separators so the name is rebuilt exactly
	var out strings.Builder
	start, swapped := 0, false
	flush := func(end int) {
		tok := stem[start:end]
		if !swapped && codes[strings.ToUpper(tok)] && start > 0 {
			out.WriteString(code)
			swapped = true
		} else {
			out.WriteString(tok)
		}
	}
	for i := 0; i < len(stem); i++ {
		if c := stem[i]; c == '-' || c == '_' || c == '.' {
			flush(i)
			out.WriteByte(c)
			start = i + 1
		}
	}
	flush(len(stem))
	if !swapped {
		return ""
	}
	return out.String() + ext
}

// calCandidates lists the per-finish master URLs for a SKU, most likely first: the site
// cover with the code swapped (catalog folder, then covers), then the plain model-code name.
func calCandidates(sku string, codes map[string]bool, cat map[string]calProduct) (code string, urls []string) {
	code, models := calSplit(sku, codes)
	if code == "" {
		return "", nil
	}
	seen := map[string]bool{}
	add := func(u string) {
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	for _, m := range models {
		for _, key := range []string{m, m + "-XX"} {
			p, ok := cat[key]
			if !ok || p.Image == "" {
				continue
			}
			if name := calCoverSwap(p.Image, code, codes); name != "" {
				add("https://images.calfaucets.com/catalog/_productLarge/" + name)
				add("https://images.calfaucets.com/covers/_productLarge/" + name)
			}
		}
	}
	for _, m := range models {
		add("https://images.calfaucets.com/catalog/_productLarge/" + m + "-" + code + ".jpg")
	}
	return code, urls
}
