// Package vendorimg fetches manufacturer master product images from brand CDNs whose URLs are
// a pure function of the SKU. One job: SKU in, white-background master on disk out. Brands
// whose URLs are not predictable (Delta's hashed Demandware folders, Hansgrohe's CELUM ids)
// belong in the page-scraping resolver (internal/webimg), not here.
package vendorimg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

// Vendor maps a brand code to its master-image URL scheme.
type Vendor struct {
	Code string // our brand code
	Host string // for rate limiting
	// URL returns the master image URL for a SKU, or "" when the SKU cannot be on this CDN.
	URL func(sku string) string
	// Resolve finds the master URL by reading the vendor's product page when the CDN key is not
	// derivable from the SKU alone (Robern names Scene7 assets "<Series>_<SKU>"). It returns
	// ("", nil) when the vendor has no page for the SKU and ErrNoScheme for an impossible SKU.
	Resolve func(ctx context.Context, c *Client, sku string) (string, error)
	Ext     string
}

// Brizo keys every variant by its full model number (65334LF-PCLHP-ECO, 81375-BL-2.5,
// 61306LF-H-PC-L, rough-in R62707 all resolve); anything with spaces or slashes is OCR junk.
var brizoSKU = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{3,20}$`)

// Vendors are verified schemes (2026-10-03). Brizo: 2000x2000 RGBA PNG, object on pure white,
// "B1" = beauty shot; https://media.brizo.com/elvis/OnWhite/{sm,md,lg}/<SKU>-B1.png.
var Vendors = []Vendor{
	{Code: "BRZ", Host: "media.brizo.com", Ext: ".png", URL: func(sku string) string {
		if !brizoSKU.MatchString(sku) {
			return ""
		}
		return "https://media.brizo.com/elvis/OnWhite/lg/" + sku + "-B1.png"
	}},
	{Code: "ROB", Host: "www.robern.com", Ext: ".png", Resolve: robernResolve},
	// Newport Brass (verified 2026-11-01): one JPEG per model and finish code,
	// /images/dynamicimages/products/newport/<model>/<model>_<finish>_large.jpg (~1200 px on
	// white). Our SKUs are the bare model (3180-5623); the master is fetched in the first finish
	// the model is offered in, polished chrome (26) first.
	{Code: "NEW", Host: "www.newportbrass.com", Ext: ".jpg", Resolve: newportResolve},
	// California Faucets: see calfaucets.go
	{Code: "CAL", Host: "images.calfaucets.com", Ext: ".jpg", Resolve: calResolve},
	// Lacava (verified 2026-11-01): /PDGImages/<SKU>.jpg for the models that have one (about one
	// in five of our SKUs; the rest answer 404).
	{Code: "LAV", Host: "www.lacava.com", Ext: ".jpg", URL: func(sku string) string {
		if !lacavaSKU.MatchString(sku) {
			return ""
		}
		return "https://www.lacava.com/PDGImages/" + sku + ".jpg"
	}},
}

var lacavaSKU = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{2,24}$`)
var newportSKU = regexp.MustCompile(`^[0-9][0-9A-Z-]{2,14}$`)

// newportFinishes are Newport Brass finish codes in the order a master is tried: polished
// chrome, satin nickel, oil rubbed bronze, forever brass, satin brass, flat black, polished nickel.
var newportFinishes = []string{"26", "15", "10", "01", "04", "56", "15S", "24", "03N", "08"}

func newportResolve(ctx context.Context, c *Client, sku string) (string, error) {
	if !newportSKU.MatchString(sku) {
		return "", ErrNoScheme
	}
	for _, f := range newportFinishes {
		u := "https://www.newportbrass.com/images/dynamicimages/products/newport/" + sku + "/" + sku + "_" + f + "_large.jpg"
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

// Robern (a Kohler company) serves product photography from Kohler's Scene7 account; the asset
// name is "<Series>_<SKU>", so the product page is read once to learn the series. The page lives
// under its category slug, which the site redirects to the canonical path.
var robernSKU = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{3,24}$`)
var robernSlugs = []string{"medicine-cabinets", "mirrors", "lighting", "vanities", "linen-cabinets", "accessories"}
var reRobernHero = regexp.MustCompile(`https://s7d4\.scene7\.com/is/image/KIG/([A-Za-z0-9_.-]+)\?[^"]*"\s+alt="3/4 view of product"`)
var reRobernAsset = regexp.MustCompile(`https://s7d4\.scene7\.com/is/image/KIG/([A-Za-z0-9_.-]+)`)

func robernResolve(ctx context.Context, c *Client, sku string) (string, error) {
	if !robernSKU.MatchString(sku) {
		return "", ErrNoScheme
	}
	// the price book is OCR'd: a trailing "O" in a Robern model is the digit 0 (MR2036D4FBEO)
	tries := []string{sku}
	if fixed := strings.NewReplacer("O", "0").Replace(sku); fixed != sku {
		tries = append(tries, fixed)
	}
	for _, try := range tries {
		if u, err := robernPage(ctx, c, try); u != "" || err != nil {
			return u, err
		}
	}
	return "", nil
}

func robernPage(ctx context.Context, c *Client, sku string) (string, error) {
	for _, slug := range robernSlugs {
		body, status, err := c.get(ctx, "https://www.robern.com/products/"+slug+"/"+strings.ToLower(sku), 2<<20)
		if err != nil {
			return "", err
		}
		if status != http.StatusOK {
			continue
		}
		asset := ""
		if m := reRobernHero.FindSubmatch(body); m != nil {
			asset = string(m[1])
		} else {
			for _, m := range reRobernAsset.FindAllSubmatch(body, -1) {
				if strings.HasSuffix(strings.ToUpper(string(m[1])), "_"+sku) {
					asset = string(m[1])
					break
				}
			}
		}
		if asset == "" {
			continue
		}
		return "https://s7d4.scene7.com/is/image/KIG/" + asset + "?wid=2000&hei=2000&fmt=png-alpha", nil
	}
	return "", nil
}

func ByCode(code string) *Vendor {
	for i := range Vendors {
		if Vendors[i].Code == code {
			return &Vendors[i]
		}
	}
	return nil
}

// ErrNoScheme means the SKU shape is not one the vendor publishes masters for.
var ErrNoScheme = errors.New("vendorimg: sku shape has no master url")

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	dir     string // masters root (dir/<code>/…); resolvers keep their lookup caches under it
}

func New(rps float64) *Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.Logger = nil
	rc.HTTPClient.Timeout = 60 * time.Second
	rc.CheckRetry = func(ctx context.Context, res *http.Response, err error) (bool, error) {
		if res != nil && (res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden) {
			return false, nil // a missing SKU is an answer, not a transient failure (Brizo says 403)
		}
		return retryablehttp.DefaultRetryPolicy(ctx, res, err)
	}
	return &Client{http: rc.StandardClient(), limiter: rate.NewLimiter(rate.Limit(rps), 1)}
}

// Result is one fetched master.
type Result struct {
	URL   string
	Path  string // absolute
	Bytes int64
}

// Fetch downloads the master for sku into dir/<code>/<SKU><ext>. It returns ErrNoScheme when
// the SKU cannot be on this CDN, (nil, nil) when the vendor has no image for it, and skips the
// network when the file already exists.
func (c *Client) Fetch(ctx context.Context, v *Vendor, sku, dir string) (*Result, error) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	c.dir = dir
	path := filepath.Join(dir, v.Code, sku+v.Ext)
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return &Result{URL: "file://" + path, Path: path, Bytes: st.Size()}, nil
	}
	u := ""
	switch {
	case v.URL != nil:
		u = v.URL(sku)
		if u == "" {
			return nil, ErrNoScheme
		}
	case v.Resolve != nil:
		var err error
		if u, err = v.Resolve(ctx, c, sku); err != nil {
			return nil, err
		}
		if u == "" {
			return nil, nil
		}
	default:
		return nil, ErrNoScheme
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (georges-showroom dealer sync)")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusForbidden {
		return nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, res.Status)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("GET %s: not an image (%s)", u, ct)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dl-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, res.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if n < 1024 {
		return nil, fmt.Errorf("GET %s: %d bytes, not a master", u, n)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, err
	}
	return &Result{URL: u, Path: path, Bytes: n}, nil
}

// get fetches a vendor page under the rate limit and returns up to max bytes of its body.
func (c *Client) get(ctx context.Context, u string, max int64) ([]byte, int, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (georges-showroom dealer sync)")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, max))
	return body, res.StatusCode, err
}
