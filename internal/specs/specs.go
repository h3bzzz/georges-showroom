// Package specs fetches manufacturer spec/installation sheets from documents.cx1accel.com
// (plain HTTP, no Cloudflare) and extracts their text with poppler's pdftotext and their
// embedded images with pdfimages.
package specs

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

const Host = "https://documents.cx1accel.com"

// NormalizeURL maps the per-site document aliases the product pages print
// (http://georges.Requested-Info.com/brands/…, which answers 403 to us) onto the CDN origin
// that serves the same path without Cloudflare.
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "/brands/"); i >= 0 {
		return Host + u[i:]
	}
	return u
}

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
}

// NewClient returns a retrying client capped at rps requests per second (0 = unlimited).
func NewClient(rps float64) *Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.Logger = nil
	rc.HTTPClient.Timeout = 90 * time.Second
	c := &Client{http: rc.StandardClient()}
	if rps > 0 {
		c.limiter = rate.NewLimiter(rate.Limit(rps), int(rps)+1)
	}
	return c
}

// Result describes a fetched document.
type Result struct {
	URL   string
	Path  string
	Bytes int
	SHA1  string
	Text  string
	Pages int
}

// LocalPath is where a document URL is stored under dir: dir/<CODE>/<specSheets|installSheets>/<file>.
func LocalPath(dir, url string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(NormalizeURL(url), Host), "/")
	rel = strings.TrimPrefix(rel, "brands/")
	rel = strings.ReplaceAll(rel, "documents/", "")
	if i := strings.IndexAny(rel, "?#"); i >= 0 {
		rel = rel[:i]
	}
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// Fetch downloads url into dir (see LocalPath) and, when extractText is set, runs
// pdftotext -layout on it. An existing local copy is reused.
func (c *Client) Fetch(ctx context.Context, url, dir string, extractText bool) (*Result, error) {
	url = NormalizeURL(url)
	path := LocalPath(dir, url)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	var body []byte
	if b, err := os.ReadFile(path); err == nil && bytes.HasPrefix(b, []byte("%PDF")) {
		body = b
	} else {
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "georges-spec-fetcher/0.2")
		res, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %s", url, res.Status)
		}
		body, err = io.ReadAll(io.LimitReader(res.Body, 200<<20))
		if err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(body, []byte("%PDF")) {
			return nil, fmt.Errorf("GET %s: not a PDF", url)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return nil, err
		}
	}
	sum := sha1.Sum(body)
	r := &Result{URL: url, Path: path, Bytes: len(body), SHA1: hex.EncodeToString(sum[:])}
	r.Pages, _ = PDFPages(ctx, path)
	if extractText {
		txt, err := PDFText(ctx, path)
		if err != nil {
			return r, err
		}
		r.Text = txt
	}
	return r, nil
}

// PDFText runs pdftotext -layout and returns stdout.
func PDFText(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext %s: %w", path, err)
	}
	return string(out), nil
}

var rePages = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

// PDFPages reads the page count with pdfinfo.
func PDFPages(ctx context.Context, path string) (int, error) {
	out, err := exec.CommandContext(ctx, "pdfinfo", path).Output()
	if err != nil {
		return 0, err
	}
	if m := rePages.FindSubmatch(out); m != nil {
		return strconv.Atoi(string(m[1]))
	}
	return 0, nil
}

// Image is one raster image embedded in a PDF.
type Image struct {
	Path string
	Page int
	W, H int
}

// Images extracts the embedded raster images of a PDF into outDir (pdfimages -png) at their
// native resolution and keeps those at least minSide px on the short side with a sane aspect
// ratio. Soft masks, stencil masks, icons and rules are deleted (pdfimages writes masks as
// separate files; `pdfimages -list` tells them apart). Vector-only sheets yield nothing.
func Images(ctx context.Context, pdfPath, outDir string, minSide int) ([]Image, error) {
	real, err := listImages(ctx, pdfPath)
	if err != nil {
		return nil, err
	}
	if len(real) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	prefix := filepath.Join(outDir, "img")
	cmd := exec.CommandContext(ctx, "pdfimages", "-png", "-p", pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdfimages %s: %w: %s", pdfPath, err, strings.TrimSpace(string(out)))
	}
	files, _ := filepath.Glob(prefix + "-*.png")
	sort.Strings(files)
	var out []Image
	for _, f := range files {
		// img-<page>-<num>.png; num is the image number in `pdfimages -list`
		parts := strings.Split(strings.TrimSuffix(filepath.Base(f), ".png"), "-")
		num := -1
		page := 0
		if len(parts) == 3 {
			page, _ = strconv.Atoi(parts[1])
			num, _ = strconv.Atoi(parts[2])
		}
		w, h, ok := pngSize(f)
		if !ok || !real[num] || w < minSide || h < minSide || w > 6*h || h > 6*w {
			_ = os.Remove(f)
			continue
		}
		out = append(out, Image{Path: f, Page: page, W: w, H: h})
	}
	return out, nil
}

// listImages returns the image numbers that are real pictures (type "image" with a colour
// space, as opposed to smask / mask / stencil entries) from `pdfimages -list`.
func listImages(ctx context.Context, pdfPath string) (map[int]bool, error) {
	out, err := exec.CommandContext(ctx, "pdfimages", "-list", pdfPath).Output()
	if err != nil {
		return nil, fmt.Errorf("pdfimages -list %s: %w", pdfPath, err)
	}
	keep := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		// page num type width height color comp bpc enc interp object ID x-ppi y-ppi size ratio
		if len(f) < 7 {
			continue
		}
		num, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		if f[2] == "image" && f[5] != "-" {
			keep[num] = true
		}
	}
	return keep, nil
}

func pngSize(path string) (int, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
