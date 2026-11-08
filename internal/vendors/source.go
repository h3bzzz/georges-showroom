package vendors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A source knows one manufacturer's per-finish image server.
type source interface {
	// init loads whatever the brand needs (a site catalog, the server's default image).
	init(ctx context.Context, f *Fetcher) error
	// candidates lists the URLs that may hold the photo of this SKU in its finish, most
	// likely first, plus the finish code the SKU carries ("" = no finish, skip).
	candidates(sku string) (code string, urls []string)
	// valid says whether a 200 image body really is the product (some servers answer an
	// unknown name with a placeholder picture and a 200).
	valid(body []byte) bool
	// file names the stored copy; it defaults to the URL's base name.
	file(u, sku, code string) string
}

func newSource(brand string) (source, error) {
	switch brand {
	case "CAL":
		return &calSource{}, nil
	case "BAL":
		return &baldwinSource{}, nil
	case "WMK":
		return &watermarkSource{}, nil
	case "ETK":
		return &emtekSource{}, nil
	case "CHI":
		return &chicagoSource{}, nil
	case "SGM":
		return &sigmaSource{}, nil
	}
	return nil, fmt.Errorf("no per-finish image source for %s (have CAL, BAL, WMK, ETK, CHI, SGM)", brand)
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// ---- California Faucets ---------------------------------------------------------------

type calSource struct {
	codes map[string]bool
	cat   map[string]calProduct
}

func (s *calSource) init(ctx context.Context, f *Fetcher) error {
	s.codes = calCodes(f.dbCodes)
	cat, err := calCatalog(ctx, f.Client, f.DataDir)
	if err != nil {
		return err
	}
	s.cat = cat
	return nil
}
func (s *calSource) candidates(sku string) (string, []string) {
	return calCandidates(sku, s.codes, s.cat)
}
func (s *calSource) valid([]byte) bool          { return true }
func (s *calSource) file(u, _, _ string) string { return baseName(u) }

// ---- Baldwin (verified 2026-11-07) -------------------------------------------------------
// Scene7: images.baldwinhardware.com/is/image/Baldwin/<model>-<finish>-c1, lower case, the
// SKU's dots as hyphens, c1 = first view. An unknown id gets a 5 KB "077 collection"
// placeholder with HTTP 200, so the body is compared against that default.

// model.finish[.option]: Baldwin's own product pages show the model in the finish with a
// default rose or trim, and the picture is the maker's; the option (rose, backset, keying)
// is not in the image id, which is why the id drops the third part.
var balSKU = regexp.MustCompile(`^([A-Z0-9]+)\.(\d{3})(\.[A-Z0-9]+)?$`)

type baldwinSource struct{ def string }

func (s *baldwinSource) init(ctx context.Context, f *Fetcher) error {
	body, ok, err := f.get(ctx, "https://images.baldwinhardware.com/is/image/Baldwin/zz-no-such-000-c1?wid=1600&fmt=png-alpha")
	if err != nil {
		return err
	}
	if ok {
		s.def = sum(body)
	}
	return nil
}
func (s *baldwinSource) candidates(sku string) (string, []string) {
	m := balSKU.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(sku)))
	if m == nil {
		return "", nil
	}
	id := strings.ToLower(m[1]) + "-" + m[2]
	return m[2], []string{"https://images.baldwinhardware.com/is/image/Baldwin/" + id + "-c1?wid=1600&fmt=png-alpha"}
}
func (s *baldwinSource) valid(body []byte) bool { return len(body) > 8000 && sum(body) != s.def }
func (s *baldwinSource) file(u, _, _ string) string {
	n := baseName(u)
	if i := strings.IndexByte(n, '?'); i >= 0 {
		n = n[:i]
	}
	return n + ".png"
}

// ---- Watermark Designs (verified 2026-11-07) ---------------------------------------------
// The public configurator renders any <spout>-<handle>-<FINISH> as an 865 px RGBA PNG at
// watermark-designs.com/configurator/faucetImage_square/<name>.png; an unknown name renders
// the default chrome faucet with HTTP 200, so the body is compared against that default.

type watermarkSource struct {
	def     string
	codes   map[string]bool // finish codes
	spouts  map[string]bool
	handles map[string]bool
}

// init reads the configurator's own data file: the renderer draws a default spout or
// handle for a code it does not know, so only SKUs made of a known spout, a known handle
// and a known finish are asked for.
func (s *watermarkSource) init(ctx context.Context, f *Fetcher) error {
	body, ok, err := f.getAny(ctx, "https://watermark-designs.com/configurator/data/data.json")
	if err != nil || !ok {
		return fmt.Errorf("watermark configurator data: %v", err)
	}
	var d struct {
		Finish map[string]json.RawMessage `json:"finish"`
		Spout  map[string]json.RawMessage `json:"spout"`
		Handle map[string]json.RawMessage `json:"handle"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return fmt.Errorf("watermark configurator data: %w", err)
	}
	up := func(m map[string]json.RawMessage) map[string]bool {
		out := map[string]bool{}
		for k := range m {
			out[strings.ToUpper(k)] = true
		}
		return out
	}
	s.codes, s.spouts, s.handles = up(d.Finish), up(d.Spout), up(d.Handle)
	body, ok, err = f.get(ctx, "https://watermark-designs.com/configurator/faucetImage_square/ZZ-NOSUCH-ZZ.png")
	if err != nil {
		return err
	}
	if ok {
		s.def = sum(body)
	}
	return nil
}

func (s *watermarkSource) candidates(sku string) (string, []string) {
	u := strings.ToUpper(strings.TrimSpace(sku))
	parts := strings.Split(u, "-")
	if len(parts) < 3 || !s.codes[parts[len(parts)-1]] {
		return "", nil
	}
	code, rest := parts[len(parts)-1], parts[:len(parts)-1]
	for k := 1; k < len(rest); k++ {
		if s.spouts[strings.Join(rest[:k], "-")] && s.handles[strings.Join(rest[k:], "-")] {
			return code, []string{"https://watermark-designs.com/configurator/faucetImage_square/" + u + ".png"}
		}
	}
	return "", nil
}
func (s *watermarkSource) valid(body []byte) bool {
	return len(body) > 8000 && s.def != "" && sum(body) != s.def
}
func (s *watermarkSource) file(u, _, _ string) string { return baseName(u) }

func baseName(u string) string {
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// ---- Chicago Faucets (map built by `vendors crawl --brand CHI`) ---------------------------
// Pictures are keyed by the exact SKU on the product page; a SKU's finish is its trailing
// code (CP, ABCP, …) and a sibling finish shares the component picture when its own is
// missing, so the SKU without its finish is the fallback key.

type chicagoSource struct{ bySKU map[string]string }

var chiCode = regexp.MustCompile(`(ABCP|ABRBF|ABRCF|RCF|ABRRCF|XKCP|XKCBCP|CP|PN|BN|SN|BB|OB|MB)$`)

func (s *chicagoSource) init(_ context.Context, f *Fetcher) error {
	b, err := os.ReadFile(filepath.Join(f.DataDir, "webimg", "CHI", "_images.json"))
	if err != nil {
		return err
	}
	var rows []ImageRow
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	s.bySKU = map[string]string{}
	for _, r := range rows {
		if r.SKU != "" && r.URL != "" {
			s.bySKU[strings.ToUpper(r.SKU)] = r.URL
		}
	}
	return nil
}
func (s *chicagoSource) candidates(sku string) (string, []string) {
	u := strings.ToUpper(strings.TrimSpace(sku))
	code := ""
	if m := chiCode.FindStringSubmatch(u); m != nil {
		code = m[1]
	}
	if url, ok := s.bySKU[u]; ok {
		return code, []string{url}
	}
	if code != "" {
		if url, ok := s.bySKU[strings.TrimSuffix(u, code)]; ok {
			return code, []string{url}
		}
		if url, ok := s.bySKU[strings.TrimSuffix(strings.TrimSuffix(u, code), "-")]; ok {
			return code, []string{url}
		}
	}
	return "", nil
}
func (s *chicagoSource) valid(body []byte) bool { return len(body) > 6000 }
func (s *chicagoSource) file(u, sku, _ string) string {
	n := baseName(u)
	if n == "" {
		n = sku + ".jpg"
	}
	return n
}

// ---- Sigma Designs (map built by `vendors crawl --brand SGM`) -----------------------------
// The site keys a picture by the base SKU ("1.22SP00"); the price book adds the finish as a
// trailing ".NN" / ".NNX" code, so the lookup strips it.

type sigmaSource struct{ bySKU map[string]string }

var sgmFinish = regexp.MustCompile(`\.(\d{2}[A-Z]?)$`)

func (s *sigmaSource) init(_ context.Context, f *Fetcher) error {
	b, err := os.ReadFile(filepath.Join(f.DataDir, "webimg", "SGM", "_images.json"))
	if err != nil {
		return err
	}
	var rows []ImageRow
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	s.bySKU = map[string]string{}
	for _, r := range rows {
		if r.SKU != "" && r.URL != "" {
			s.bySKU[strings.ToUpper(r.SKU)] = r.URL
		}
	}
	return nil
}
func (s *sigmaSource) candidates(sku string) (string, []string) {
	u := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(sku)), " ", "")
	if url, ok := s.bySKU[u]; ok {
		return "", []string{url}
	}
	if m := sgmFinish.FindStringSubmatch(u); m != nil {
		if url, ok := s.bySKU[strings.TrimSuffix(u, m[0])]; ok {
			return m[1], []string{url}
		}
	}
	return "", nil
}
func (s *sigmaSource) valid(body []byte) bool { return len(body) > 6000 }
func (s *sigmaSource) file(u, sku, _ string) string {
	if n := baseName(u); n != "" {
		return n
	}
	return sku + ".jpg"
}
