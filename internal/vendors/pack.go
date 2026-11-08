package vendors

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "github.com/gen2brain/webp"
	_ "golang.org/x/image/tiff"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pack imports a manufacturer image pack: the zip or folder a brand's dealer portal hands out
// (Delta/Brizo Asset Library, Kohler Content Hub, Hansgrohe, Duravit Pro, Emtek, Blanco…), with
// files named by SKU or model. Each picture is matched to our items — exact SKU first, then
// the SKU without its finish code, then the model — stored as a vendor master and linked to
// every matching item, so the card and the finish buttons show the maker's own photograph.
type Pack struct {
	Pool    *pgxpool.Pool
	DataDir string
	Brand   string // Unilog brand code
	Path    string // zip file or folder
	DryRun  bool
	// MinSide rejects thumbnails (default 400 px).
	MinSide int
}

// PackStats summarise one import.
type PackStats struct {
	Files, Pictures, Matched, Items, Skipped, Small, Unmatched int
	UnmatchedNames                                             []string
}

var reNorm2 = regexp.MustCompile(`[^A-Z0-9]`)

func normKey(s string) string { return reNorm2.ReplaceAllString(strings.ToUpper(s), "") }

// Run walks the pack.
func (p *Pack) Run(ctx context.Context) (*PackStats, error) {
	if p.MinSide <= 0 {
		p.MinSide = 400
	}
	st := &PackStats{}
	var brandID int
	if err := p.Pool.QueryRow(ctx, `SELECT id FROM brands WHERE code=$1`, p.Brand).Scan(&brandID); err != nil {
		return nil, fmt.Errorf("brand %s: %w", p.Brand, err)
	}
	// every item of the brand under three keys: exact SKU, SKU minus finish, model prefix
	type it struct {
		id, familyID int64
		sku          string
	}
	bySKU := map[string][]it{}
	byBase := map[string][]it{}
	codes := map[string]bool{}
	cr, err := p.Pool.Query(ctx, `SELECT code FROM finish_codes WHERE brand_code=$1 OR brand_code='*'`, p.Brand)
	if err != nil {
		return nil, err
	}
	for cr.Next() {
		var c string
		if cr.Scan(&c) == nil {
			codes[strings.ToUpper(c)] = true
		}
	}
	cr.Close()
	rows, err := p.Pool.Query(ctx, `SELECT i.id, i.family_id, i.sku, COALESCE(i.finish_code,'') FROM items i WHERE i.brand_id=$1`, brandID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x it
		var fcode string
		if err := rows.Scan(&x.id, &x.familyID, &x.sku, &fcode); err != nil {
			rows.Close()
			return nil, err
		}
		k := normKey(x.sku)
		bySKU[k] = append(bySKU[k], x)
		for _, b := range skuBases(x.sku, fcode, codes) {
			byBase[b] = append(byBase[b], x)
		}
	}
	rows.Close()
	slog.Info("image pack", "brand", p.Brand, "items", len(bySKU), "bases", len(byBase), "path", p.Path)

	// walk the pack
	type file struct {
		name string
		open func() (io.ReadCloser, error)
	}
	var files []file
	info, err := os.Stat(p.Path)
	if err != nil {
		return nil, err
	}
	var zr *zip.ReadCloser
	if !info.IsDir() && strings.EqualFold(filepath.Ext(p.Path), ".zip") {
		zr, err = zip.OpenReader(p.Path)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			f := f
			files = append(files, file{f.Name, func() (io.ReadCloser, error) { return f.Open() }})
		}
	} else {
		err := filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			files = append(files, file{path, func() (io.ReadCloser, error) { return os.Open(path) }})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	for _, f := range files {
		if ctx.Err() != nil {
			return st, ctx.Err()
		}
		st.Files++
		ext := strings.ToLower(filepath.Ext(f.name))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".tif", ".tiff", ".gif":
		default:
			st.Skipped++
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(f.name), filepath.Ext(f.name))
		// trailing view markers common in packs: _1, -2, _front, _3q, (1)
		stem = reView.ReplaceAllString(stem, "")
		key := normKey(stem)
		var hits []it
		how := ""
		if h, ok := bySKU[key]; ok {
			hits, how = h, "vendor"
		} else if h, ok := byBase[key]; ok {
			hits, how = h, "vendor"
		} else {
			// the file may name the model with its finish glued differently; try peeling
			// known finish codes off the end
			for code := range codes {
				if strings.HasSuffix(key, code) && len(key) > len(code)+2 {
					if h, ok := byBase[strings.TrimSuffix(key, code)]; ok {
						hits, how = h, "vendor"
						break
					}
				}
			}
		}
		if len(hits) == 0 {
			st.Unmatched++
			if len(st.UnmatchedNames) < 40 {
				st.UnmatchedNames = append(st.UnmatchedNames, filepath.Base(f.name))
			}
			continue
		}
		st.Pictures++
		rc, err := f.open()
		if err != nil {
			return st, err
		}
		body, err := io.ReadAll(io.LimitReader(rc, 60<<20))
		rc.Close()
		if err != nil {
			return st, err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || min(cfg.Width, cfg.Height) < p.MinSide {
			st.Small++
			continue
		}
		st.Matched++
		if p.DryRun {
			st.Items += len(hits)
			continue
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		rel := filepath.ToSlash(filepath.Join("webimg", p.Brand, "pack", normKey(stem)+"."+format))
		abs := filepath.Join(p.DataDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return st, err
		}
		if _, err := os.Stat(abs); err != nil {
			if err := os.WriteFile(abs, body, 0o644); err != nil {
				return st, err
			}
		}
		origin, _ := json.Marshal(map[string]any{"url": "pack:" + filepath.Base(p.Path) + "/" + filepath.Base(f.name), "finish_code": finishOf(stem, codes)})
		var aid int64
		if err := p.Pool.QueryRow(ctx, `
			INSERT INTO assets (hash, src_path, format, width, height, bytes, source, origin)
			VALUES ($1,$2,$3,$4,$5,$6,'vendor',$7)
			ON CONFLICT (hash) DO UPDATE SET origin = assets.origin || EXCLUDED.origin RETURNING id`,
			hash, rel, format, cfg.Width, cfg.Height, len(body), string(origin)).Scan(&aid); err != nil {
			return st, err
		}
		for _, h := range hits {
			if _, err := p.Pool.Exec(ctx, `INSERT INTO asset_links (asset_id, family_id, item_id, how, weight) VALUES ($1,$2,$3,$4,1) ON CONFLICT DO NOTHING`, aid, h.familyID, h.id, how); err != nil {
				return st, err
			}
			st.Items++
		}
	}
	return st, nil
}

var reView = regexp.MustCompile(`(?i)([_\-\s]+(\d{1,2}|front|side|back|top|3q|3-4|three[_\-]?quarter|main|alt\d*|hero|detail|lifestyle|room|install(ed)?)|\s*\(\d+\))$`)

// skuBases lists the finish-stripped keys of a SKU: with the known finish code removed, and
// with the last token removed when it looks like a code.
func skuBases(sku, fcode string, codes map[string]bool) []string {
	u := strings.ToUpper(strings.TrimSpace(sku))
	var out []string
	add := func(s string) {
		if k := normKey(s); k != "" && k != normKey(u) {
			out = append(out, k)
		}
	}
	if fcode != "" {
		if i := strings.LastIndex(u, fcode); i > 0 {
			add(u[:i])
		}
	}
	toks := reTok2.Split(u, -1)
	if len(toks) >= 2 {
		last := toks[len(toks)-1]
		if codes[last] || len(last) <= 4 {
			add(strings.Join(toks[:len(toks)-1], "-"))
		}
		first := toks[0]
		if codes[first] && len(toks) >= 2 {
			add(strings.Join(toks[1:], "-"))
		}
	}
	return out
}

var reTok2 = regexp.MustCompile(`[.\-/ _]+`)

// finishOf names the finish code a file name ends with, when it is a known one.
func finishOf(stem string, codes map[string]bool) string {
	toks := reTok2.Split(strings.ToUpper(stem), -1)
	if len(toks) < 2 {
		return ""
	}
	if last := toks[len(toks)-1]; codes[last] {
		return last
	}
	return ""
}
