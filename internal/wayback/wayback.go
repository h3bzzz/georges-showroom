// Package wayback pulls archived copies of georgesshowroom.com product pages from the Internet
// Archive. It is a bootstrap for structured product data (SKU, brand, category, price) while the
// daily price file is not wired up yet; it never supplies images.
package wayback

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

type Snapshot struct {
	URL       string
	Timestamp string // yyyymmddhhmmss
}

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
}

func New(rps float64) *Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.Logger = nil
	rc.HTTPClient.Timeout = 60 * time.Second
	return &Client{http: rc.StandardClient(), limiter: rate.NewLimiter(rate.Limit(rps), 2)}
}

// ListProducts returns the newest 200-status snapshot of every archived product page.
func (c *Client) ListProducts(ctx context.Context, limit int) ([]Snapshot, error) {
	q := url.Values{}
	q.Set("url", "georgesshowroom.com/Pasadena-California/*")
	q.Set("output", "txt")
	q.Set("fl", "original,timestamp,statuscode,mimetype")
	q.Set("collapse", "urlkey")
	q.Set("filter", "statuscode:200")
	q.Set("filter", "mimetype:text/html")
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://web.archive.org/cdx/search/cdx?"+q.Encode(), nil)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out []Snapshot
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || strings.Contains(f[0], "iAmHuman") || !strings.HasSuffix(strings.ToUpper(f[0]), ".HTM") {
			continue
		}
		out = append(out, Snapshot{URL: f[0], Timestamp: f[1]})
	}
	return out, sc.Err()
}

// Fetch returns the archived HTML (original bytes, no toolbar), transparently gunzipping.
func (c *Client) Fetch(ctx context.Context, s Snapshot) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("http://web.archive.org/web/%sid_/%s", s.Timestamp, s.URL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wayback %s: %s", s.URL, res.Status)
	}
	var r io.Reader = res.Body
	br := bufio.NewReader(res.Body)
	if b, _ := br.Peek(2); len(b) == 2 && b[0] == 0x1f && b[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	} else {
		r = br
	}
	return io.ReadAll(io.LimitReader(r, 4<<20))
}

// ListFromFile reads a CDX text dump ("original timestamp status mimetype" per line).
func ListFromFile(path string) ([]Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 2 || strings.Contains(fl[0], "iAmHuman") || !strings.HasSuffix(strings.ToUpper(strings.SplitN(fl[0], "?", 2)[0]), ".HTM") {
			continue
		}
		out = append(out, Snapshot{URL: fl[0], Timestamp: fl[1]})
	}
	return out, sc.Err()
}
