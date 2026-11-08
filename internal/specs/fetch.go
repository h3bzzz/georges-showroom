package specs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

// Fetcher asks manufacturers for sheets, one model at a time, politely.
type Fetcher struct {
	Pool    *pgxpool.Pool
	DataDir string
	Brand   string // Unilog brand code
	Limit   int
	DryRun  bool
	Client  *http.Client
	lim     *rate.Limiter
}

type target struct {
	familyID int64
	itemID   int64
	sku      string
	category string
}

// Run fetches sheets for families of the brand that have none. Returns found, tried.
func (f *Fetcher) Run(ctx context.Context) (int, int, error) {
	res, ok := Resolvers[f.Brand]
	if !ok {
		return 0, 0, fmt.Errorf("no resolver for brand %s (have: %s)", f.Brand, strings.Join(keys(), ", "))
	}
	if f.Client == nil {
		f.Client = &http.Client{Timeout: 25 * time.Second}
	}
	f.lim = rate.NewLimiter(rate.Every(500*time.Millisecond), 1)
	finishes := map[string]bool{}
	fr, err := f.Pool.Query(ctx, `SELECT code FROM finish_codes WHERE brand_code=$1 OR brand_code='*'`, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	for fr.Next() {
		var c string
		if fr.Scan(&c) == nil {
			finishes[strings.ToUpper(c)] = true
		}
	}
	fr.Close()
	limit := ""
	if f.Limit > 0 {
		limit = " LIMIT " + strconv.Itoa(f.Limit)
	}
	// one representative SKU per family that has no sheet yet; fixtures first
	rows, err := f.Pool.Query(ctx, `
		SELECT DISTINCT ON (fam.id) fam.id, i.id, i.sku, COALESCE(fam.category,'')
		FROM families fam JOIN brands b ON b.id=fam.brand_id JOIN items i ON i.family_id=fam.id
		WHERE b.code=$1 AND fam.tier <> 3
		  AND NOT EXISTS (SELECT 1 FROM documents d WHERE d.family_id=fam.id AND d.kind='spec')
		ORDER BY fam.id, (i.price_cents IS NOT NULL) DESC, i.sku`+limit, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.familyID, &t.itemID, &t.sku, &t.category); err != nil {
			rows.Close()
			return 0, 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()
	tried := map[string]bool{}
	lr, err := f.Pool.Query(ctx, `SELECT model FROM spec_lookups WHERE brand_code=$1`, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	for lr.Next() {
		var m string
		if lr.Scan(&m) == nil {
			tried[m] = true
		}
	}
	lr.Close()
	found, n := 0, 0
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		cands := res(t.sku, t.category, finishes)
		if len(cands) == 0 {
			continue
		}
		key := cands[len(cands)-1].Name // the base model's name identifies the lookup
		if tried[key] {
			continue
		}
		tried[key] = true
		n++
		var hit *Candidate
		var body []byte
		gated := false
		for attempt := 0; attempt < 2 && hit == nil; attempt++ {
			gated = false
			for i := range cands {
				b, ok, gate := f.get(ctx, cands[i].URL)
				if gate {
					gated = true
					break
				}
				if ok {
					hit, body = &cands[i], b
					break
				}
			}
			if !gated {
				break
			}
			if attempt == 0 {
				// a bot gate (Imperva, Cloudflare challenge) after a burst: wait it out once
				slog.Warn("specs fetch: bot gate, pausing", "brand", f.Brand, "url", cands[0].URL)
				select {
				case <-time.After(90 * time.Second):
				case <-ctx.Done():
				}
			}
		}
		if gated {
			// still gated: stop the brand rather than hammer it; the rest needs a browser pass
			slog.Warn("specs fetch: bot gate persists, stopping brand", "brand", f.Brand)
			n--
			break
		}
		if hit == nil {
			f.Pool.Exec(ctx, `INSERT INTO spec_lookups (brand_code, model, found) VALUES ($1,$2,false) ON CONFLICT (brand_code, model) DO UPDATE SET found=false, tried_at=now()`, f.Brand, key)
			continue
		}
		found++
		if f.DryRun {
			slog.Info("would store", "sku", t.sku, "url", hit.URL, "bytes", len(body))
			continue
		}
		rel := filepath.ToSlash(filepath.Join("docs", f.Brand, safeName(hit.Name)))
		abs := filepath.Join(f.DataDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return found, n, err
		}
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return found, n, err
		}
		pages, text := pdfText(abs)
		_, err := f.Pool.Exec(ctx, `
			INSERT INTO documents (family_id, item_id, kind, url, path, pages, text)
			VALUES ($1,$2,'spec',$3,$4,$5,NULLIF($6,''))
			ON CONFLICT (url) DO UPDATE SET family_id=COALESCE(documents.family_id, EXCLUDED.family_id), path=EXCLUDED.path, pages=EXCLUDED.pages, text=EXCLUDED.text`,
			t.familyID, t.itemID, hit.URL, rel, pages, text)
		if err != nil {
			return found, n, err
		}
		f.Pool.Exec(ctx, `INSERT INTO spec_lookups (brand_code, model, found, url) VALUES ($1,$2,true,$3) ON CONFLICT (brand_code, model) DO UPDATE SET found=true, url=EXCLUDED.url, tried_at=now()`, f.Brand, key, hit.URL)
		if found%25 == 0 {
			slog.Info("specs fetch", "brand", f.Brand, "found", found, "tried", n, "of", len(targets))
		}
	}
	slog.Info("specs fetch done", "brand", f.Brand, "found", found, "tried", n, "families", len(targets))
	return found, n, nil
}

// get fetches a candidate and returns the bytes when it is a real PDF.
func (f *Fetcher) get(ctx context.Context, u string) (body []byte, ok, gated bool) {
	if err := f.lim.Wait(ctx); err != nil {
		return nil, false, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, false
	}
	req.Header.Set("User-Agent", "GeorgesShowroom/2 (spec sheet fetch; +https://georgesshowroom.com)")
	req.Header.Set("Accept", "application/pdf,*/*;q=0.5")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, false, isGate(resp, peek)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	if err != nil || len(body) < 1024 || !strings.HasPrefix(string(body[:5]), "%PDF-") {
		return nil, false, isGate(resp, body)
	}
	return body, true, false
}

// isGate recognises a bot-detection interstitial served in place of the file.
func isGate(resp *http.Response, body []byte) bool {
	if resp.Header.Get("cf-mitigated") == "challenge" {
		return true
	}
	head := string(body[:min(len(body), 4096)])
	for _, mark := range []string{"_Incapsula_Resource", "Pardon Our Interruption", "reeseSkipExpirationCheck", "cf-chl-", "Just a moment..."} {
		if strings.Contains(head, mark) {
			return true
		}
	}
	return false
}

func safeName(n string) string {
	n = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, n)
	return n
}

// pdfText extracts page count and text with poppler when installed; both are optional.
func pdfText(abs string) (*int, string) {
	var pages *int
	if out, err := exec.Command("pdfinfo", abs).Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(l, "Pages:") {
				if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, "Pages:"))); err == nil {
					pages = &n
				}
			}
		}
	}
	text := ""
	if out, err := exec.Command("pdftotext", "-layout", "-l", "6", abs, "-").Output(); err == nil {
		text = string(out)
		if len(text) > 60000 {
			text = text[:60000]
		}
	}
	return pages, text
}

func keys() []string {
	var out []string
	for k := range Resolvers {
		out = append(out, k)
	}
	return out
}
