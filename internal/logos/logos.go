// Package logos fetches manufacturer logos from the manufacturers' own websites: the first
// header image whose src/alt/class mentions "logo", else the site's apple-touch-icon, else a
// public favicon service. Nothing is taken from the old storefront.
package logos

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"regexp"

	_ "golang.org/x/image/webp"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Result struct {
	URL   string
	Bytes []byte
	Ext   string // .svg .png .jpg .webp .ico
	How   string // logo-img | touch-icon | favicon-service
}

type Client struct{ http *http.Client }

func New() *Client {
	return &Client{http: &http.Client{Timeout: 25 * time.Second}}
}

func (c *Client) get(ctx context.Context, u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
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
	b, err := io.ReadAll(io.LimitReader(res.Body, 6<<20))
	return b, res.Header.Get("Content-Type"), err
}

func extFor(ct string, u string, body []byte) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "svg") || bytes.Contains(body[:min(len(body), 300)], []byte("<svg")):
		return ".svg"
	case strings.Contains(ct, "png") || bytes.HasPrefix(body, []byte("\x89PNG")):
		return ".png"
	case strings.Contains(ct, "jpeg") || bytes.HasPrefix(body, []byte("\xff\xd8")):
		return ".jpg"
	case strings.Contains(ct, "webp") || (len(body) > 12 && string(body[8:12]) == "WEBP"):
		return ".webp"
	case strings.Contains(ct, "gif") || bytes.HasPrefix(body, []byte("GIF8")):
		return ".gif"
	case strings.Contains(ct, "icon") || strings.HasSuffix(strings.ToLower(u), ".ico"):
		return ".ico"
	}
	return ""
}

// Fetch tries the site's own logo first, then its touch icon, then a favicon service.
func (c *Client) Fetch(ctx context.Context, domain string) (*Result, error) {
	base := "https://" + domain + "/"
	if page, _, err := c.get(ctx, base); err == nil {
		if doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page)); err == nil {
			var cands []string
			doc.Find("header img, nav img, .header img, .navbar img, .logo img, a[href='/'] img, img").EachWithBreak(func(_ int, s *goquery.Selection) bool {
				// lazy-loaded sites put a blank placeholder in src and the real file in data-* attributes
				src := firstNonEmpty(s.AttrOr("data-src", ""), s.AttrOr("data-lazy-src", ""), s.AttrOr("data-original", ""), firstSrcset(s.AttrOr("data-srcset", "")), firstSrcset(s.AttrOr("srcset", "")), s.AttrOr("src", ""))
				meta := strings.ToLower(s.AttrOr("alt", "") + " " + s.AttrOr("class", "") + " " + s.AttrOr("id", "") + " " + src + " " + s.Parent().AttrOr("class", ""))
				if src != "" && strings.Contains(meta, "logo") && !strings.Contains(meta, "payment") {
					if strings.Contains(meta, "white") || strings.Contains(meta, "_wh") || strings.Contains(meta, "-wh") || strings.Contains(meta, "light") || strings.Contains(meta, "inverse") || strings.Contains(meta, "footer") {
						cands = append(cands, src) // last resort
					} else {
						cands = append([]string{src}, cands...)
					}
				}
				return len(cands) < 6
			})
			doc.Find("meta[property='og:logo'], link[rel='logo']").Each(func(_ int, s *goquery.Selection) {
				if v := firstNonEmpty(s.AttrOr("content", ""), s.AttrOr("href", "")); v != "" {
					cands = append(cands, v)
				}
			})
			for _, src := range cands {
				u := resolve(base, src)
				if u == "" || strings.HasPrefix(u, "data:") {
					continue
				}
				if body, ct, err := c.get(ctx, u); err == nil && len(body) > 1500 {
					if ext := extFor(ct, u, body); ext != "" && ext != ".ico" {
						if ext == ".svg" && looksWhite(body, ext) {
							body = DarkenSVG(body) // white-on-dark variant: recolour so it reads on white
						}
						if !looksWhite(body, ext) {
							return &Result{URL: u, Bytes: body, Ext: ext, How: "logo-img"}, nil
						}
					}
				}
			}
			var icons []string
			doc.Find("link[rel*='apple-touch-icon'], link[rel='icon'][sizes]").Each(func(_ int, s *goquery.Selection) {
				icons = append(icons, s.AttrOr("href", ""))
			})
			for _, h := range icons {
				u := resolve(base, h)
				if body, ct, err := c.get(ctx, u); err == nil && len(body) > 500 {
					if ext := extFor(ct, u, body); ext != "" {
						return &Result{URL: u, Bytes: body, Ext: ext, How: "touch-icon"}, nil
					}
				}
			}
		}
	}
	u := "https://icons.duckduckgo.com/ip3/" + domain + ".ico"
	if body, ct, err := c.get(ctx, u); err == nil && len(body) > 300 {
		ext := extFor(ct, u, body)
		if ext == "" {
			ext = ".ico"
		}
		return &Result{URL: u, Bytes: body, Ext: ext, How: "favicon-service"}, nil
	}
	return nil, fmt.Errorf("no logo found for %s", domain)
}

// firstSrcset returns the first URL of a srcset attribute.
func firstSrcset(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	first := strings.Split(v, ",")[0]
	return strings.Fields(first)[0]
}

func resolve(base, src string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return ""
	}
	return b.ResolveReference(u).String()
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

// looksWhite reports whether a raster logo is essentially white (a variant meant for dark
// backgrounds). SVGs are checked for a white fill without any dark colour.
func looksWhite(b []byte, ext string) bool {
	if ext == ".svg" {
		s := strings.ToLower(string(b))
		return strings.Contains(s, "#fff") && !strings.ContainsAny(s, "") && !strings.Contains(s, "#000") && !strings.Contains(s, "#1") && !strings.Contains(s, "#2") && !strings.Contains(s, "#3")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return false
	}
	bd := img.Bounds()
	n, bright := 0, 0
	for y := bd.Min.Y; y < bd.Max.Y; y += 3 {
		for x := bd.Min.X; x < bd.Max.X; x += 3 {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x4000 {
				continue
			}
			n++
			if r>>8 > 225 && g>>8 > 225 && bl>>8 > 225 {
				bright++
			}
		}
	}
	if n < 200 {
		return true // (almost) fully transparent placeholder
	}
	return float64(bright)/float64(n) > 0.9
}

// FetchURL downloads one explicit logo URL (override list).
func (c *Client) FetchURL(ctx context.Context, u string) (*Result, error) {
	body, ct, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	ext := extFor(ct, u, body)
	if ext == "" || len(body) < 500 {
		return nil, fmt.Errorf("%s: not an image", u)
	}
	return &Result{URL: u, Bytes: body, Ext: ext, How: "override"}, nil
}

var reWhiteFill = regexp.MustCompile(`(?i)(fill|stroke)(\s*[:=]\s*"?'?\s*)(#fff(?:fff)?|white|rgb\(\s*255\s*,\s*255\s*,\s*255\s*\))`)

// DarkenSVG rewrites white fills/strokes to near-black so a reversed logo reads on white.
func DarkenSVG(b []byte) []byte {
	return reWhiteFill.ReplaceAll(b, []byte("${1}${2}#1c1f22"))
}
