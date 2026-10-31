package logos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Audit decides whether a stored logo file will read on a white page: raster files must be at
// least 40px wide, mostly opaque and not near-white; SVGs must contain drawable content and a
// non-white fill or stroke. ICO favicons never pass.
func Audit(path string) (ok bool, reason string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, "unreadable"
	}
	ext := strings.ToLower(path[strings.LastIndex(path, "."):])
	switch ext {
	case ".ico":
		return false, "favicon"
	case ".svg":
		s := strings.ToLower(string(b))
		if !strings.Contains(s, "<path") && !strings.Contains(s, "<polygon") && !strings.Contains(s, "<rect") && !strings.Contains(s, "<text") && !strings.Contains(s, "<circle") && !strings.Contains(s, "<image") {
			return false, "svg without shapes"
		}
		if strings.Contains(s, "<image") && !strings.Contains(s, "<path") {
			return false, "svg wrapping an external image"
		}
		if svgAllWhite(s) {
			return false, "white svg"
		}
		return true, ""
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return false, "undecodable"
	}
	bd := img.Bounds()
	if bd.Dx() < 40 || bd.Dy() < 12 {
		return false, fmt.Sprintf("too small %dx%d", bd.Dx(), bd.Dy())
	}
	n, bright, total := 0, 0, 0
	for y := bd.Min.Y; y < bd.Max.Y; y += 2 {
		for x := bd.Min.X; x < bd.Max.X; x += 2 {
			total++
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x4000 {
				continue
			}
			n++
			if r>>8 > 230 && g>>8 > 230 && bl>>8 > 230 {
				bright++
			}
		}
	}
	if n < 200 || float64(n)/float64(total) < 0.005 {
		return false, "transparent"
	}
	if float64(bright)/float64(n) > 0.92 {
		return false, "white logo"
	}
	return true, ""
}

var reFill = regexp.MustCompile(`(?i)(fill|stroke)\s*[:=]\s*"?'?\s*(#[0-9a-f]{3,8}|rgb[^;"')]*|white|black|[a-z]+)`)

func svgAllWhite(s string) bool {
	m := reFill.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return false // no explicit colours: renders black by default
	}
	dark := 0
	for _, x := range m {
		v := strings.ToLower(strings.TrimSpace(x[2]))
		switch {
		case v == "none" || v == "transparent":
			continue
		case v == "#fff" || v == "#ffffff" || v == "white" || strings.HasPrefix(v, "#ffffff"):
			continue
		default:
			dark++
		}
	}
	return dark == 0
}

// Wikipedia fetches the page image of the brand's Wikipedia article as a last resort.
func (c *Client) Wikipedia(ctx context.Context, name string) (*Result, error) {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("format", "json")
	q.Set("generator", "search")
	q.Set("gsrsearch", name+" company")
	q.Set("gsrlimit", "3")
	q.Set("prop", "pageimages")
	q.Set("piprop", "original")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://en.wikipedia.org/w/api.php?"+q.Encode(), nil)
	req.Header.Set("User-Agent", "georges-logo-fetcher/0.1 (showroom catalog; contact via site)")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var payload struct {
		Query struct {
			Pages map[string]struct {
				Title    string `json:"title"`
				Original struct {
					Source string `json:"source"`
				} `json:"original"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, p := range payload.Query.Pages {
		// the article title must carry the whole brand name, otherwise "Electric Mirror"
		// matches some unrelated lighting company
		if p.Original.Source == "" || !strings.Contains(strings.ToLower(p.Title), want) {
			continue
		}
		src := p.Original.Source
		if !strings.Contains(strings.ToLower(src), "logo") {
			continue // page image is a photo, not the mark
		}
		body, ct, err := c.get(ctx, src)
		if err != nil || len(body) < 500 {
			continue
		}
		ext := extFor(ct, src, body)
		if ext == "" || ext == ".ico" {
			continue
		}
		return &Result{URL: src, Bytes: body, Ext: ext, How: "wikipedia"}, nil
	}
	return nil, fmt.Errorf("no wikipedia logo for %s", name)
}
