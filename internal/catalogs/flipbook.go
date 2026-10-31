// Package catalogs downloads Unilog vendor catalogs, which are FlipHTML5 flipbooks hosted on
// catalogs.myplumbingshowroom.com (no Cloudflare). Layout per book:
//
//	<base>/javascript/config.js            totalPagesCaption:"597", CreatedTime:"2026…"
//	<base>/files/large/N.jpg               full-resolution page render (~2200x1700)
//	<base>/files/thumb/N.jpg               thumbnail
//	<base>/files/search/book_config.js     var textForPages = ["…", …]  (empty for some books)
package catalogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

const Host = "https://catalogs.myplumbingshowroom.com"

var (
	reTotalPages  = regexp.MustCompile(`totalPagesCaption:"(\d+)"`)
	reCreated     = regexp.MustCompile(`CreatedTime:"(\d+)"`)
	reTitle       = regexp.MustCompile(`(?i)bookTitle\s*:\s*"([^"]*)"`)
	reTextForPage = regexp.MustCompile(`(?s)textForPages\s*=\s*(\[.*?\]);`)
	reHTMLTitle   = regexp.MustCompile(`(?is)<title>\s*(.*?)\s*</title>`)
)

// Book identifies one flipbook as "<BRANDCODE>/<name>", e.g. "GES/Art_for_Chefs".
type Book struct {
	Code string // brand code, e.g. GES
	Name string // path segment after the code
}

func ParseBook(s string) (Book, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, Host), "/")
	code, name, ok := strings.Cut(s, "/")
	if !ok || code == "" || name == "" {
		return Book{}, fmt.Errorf("catalogs: want CODE/name, got %q", s)
	}
	return Book{Code: code, Name: strings.TrimSuffix(name, "/")}, nil
}

func (b Book) String() string { return b.Code + "/" + b.Name }
func (b Book) BaseURL() string {
	return Host + "/" + b.Code + "/" + strings.ReplaceAll(b.Name, " ", "%20")
}

// Info is what config.js tells us about a book.
type Info struct {
	Book      Book
	Title     string
	Pages     int
	CreatedAt string // raw yyyymmddhhmmss
	HasText   bool
	PageTexts []string // index 0 = page 1; empty strings when the book has no text layer
}

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
}

// NewClient returns a retrying, rate-limited client (rps requests per second).
func NewClient(rps float64) *Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 4
	rc.Logger = nil
	rc.HTTPClient.Timeout = 60 * time.Second
	return &Client{http: rc.StandardClient(), limiter: rate.NewLimiter(rate.Limit(rps), int(rps)+1)}
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "georges-catalog-fetcher/0.1")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(res.Body)
}

// Probe reads config.js and book_config.js.
func (c *Client) Probe(ctx context.Context, b Book) (*Info, error) {
	cfg, err := c.get(ctx, b.BaseURL()+"/javascript/config.js")
	if err != nil {
		return nil, err
	}
	info := &Info{Book: b}
	if m := reTotalPages.FindSubmatch(cfg); m != nil {
		info.Pages, _ = strconv.Atoi(string(m[1]))
	}
	if info.Pages == 0 {
		return nil, errors.New("catalogs: totalPagesCaption not found in config.js")
	}
	if m := reCreated.FindSubmatch(cfg); m != nil {
		info.CreatedAt = string(m[1])
	}
	if m := reTitle.FindSubmatch(cfg); m != nil {
		info.Title = string(m[1])
	}
	if info.Title == "" {
		if idx, err := c.get(ctx, b.BaseURL()+"/"); err == nil {
			if m := reHTMLTitle.FindSubmatch(idx); m != nil {
				info.Title = strings.TrimSpace(html.UnescapeString(string(m[1])))
			}
		}
	}
	if txt, err := c.get(ctx, b.BaseURL()+"/files/search/book_config.js"); err == nil {
		if m := reTextForPage.FindSubmatch(txt); m != nil {
			var pages []string
			if json.Unmarshal(m[1], &pages) == nil {
				info.PageTexts = pages
				for _, p := range pages {
					if strings.TrimSpace(p) != "" {
						info.HasText = true
						break
					}
				}
			}
		}
	}
	return info, nil
}

// PageURL returns the full-size render for page n (1-based).
func (b Book) PageURL(n int) string { return fmt.Sprintf("%s/files/large/%d.jpg", b.BaseURL(), n) }

// FetchPages downloads pages [from,to] into dir/<CODE>/<name>/NNNN.jpg, skipping files that
// already exist. workers bounds parallelism; the client's limiter bounds throughput.
func (c *Client) FetchPages(ctx context.Context, b Book, from, to int, dir string, workers int, progress func(page int, bytes int, skipped bool)) error {
	out := filepath.Join(dir, b.Code, safeName(b.Name))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for n := from; n <= to; n++ {
		n := n
		g.Go(func() error {
			path := filepath.Join(out, fmt.Sprintf("%04d.jpg", n))
			if st, err := os.Stat(path); err == nil && st.Size() > 0 {
				if progress != nil {
					progress(n, int(st.Size()), true)
				}
				return nil
			}
			body, err := c.get(ctx, b.PageURL(n))
			if err != nil {
				return fmt.Errorf("page %d: %w", n, err)
			}
			if err := os.WriteFile(path+".part", body, 0o644); err != nil {
				return err
			}
			if err := os.Rename(path+".part", path); err != nil {
				return err
			}
			if progress != nil {
				progress(n, len(body), false)
			}
			return nil
		})
	}
	return g.Wait()
}

// WriteText stores the text layer (if any) next to the page images as text/NNNN.txt.
func WriteText(info *Info, dir string) (int, error) {
	if !info.HasText {
		return 0, nil
	}
	out := filepath.Join(dir, info.Book.Code, safeName(info.Book.Name), "text")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for i, t := range info.PageTexts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("%04d.txt", i+1)), []byte(t), 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

// ThumbURL is the small cover image for page n.
func (b Book) ThumbURL(n int) string { return fmt.Sprintf("%s/files/thumb/%d.jpg", b.BaseURL(), n) }

// FetchThumb stores the cover thumbnail as dir/<CODE>/<name>/thumb.jpg (skips if present).
func (c *Client) FetchThumb(ctx context.Context, b Book, dir string) (string, error) {
	out := filepath.Join(dir, b.Code, safeName(b.Name), "thumb.jpg")
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out, nil
	}
	body, err := c.get(ctx, b.ThumbURL(1))
	if err != nil {
		body, err = c.get(ctx, b.PageURL(1))
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	return out, os.WriteFile(out, body, 0o644)
}
