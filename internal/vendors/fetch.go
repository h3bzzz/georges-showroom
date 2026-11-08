// Package vendors fetches manufacturers' own per-finish product photographs and links them
// to the exact SKU, so a finish button on the product page can show the real picture.
package vendors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

type Fetcher struct {
	Pool    *pgxpool.Pool
	DataDir string
	Brand   string // CAL
	Limit   int
	DryRun  bool
	Workers int
	Client  *http.Client
	lim     *rate.Limiter
	dbCodes []string
	src     source
}

type target struct {
	itemID, familyID int64
	sku              string
	have             []string // origin urls of vendor assets already linked to the item
}

// Run fetches a per-finish master for every SKU of the brand that lacks one.
func (f *Fetcher) Run(ctx context.Context) (found, tried int, err error) {
	src, err := newSource(f.Brand)
	if err != nil {
		return 0, 0, err
	}
	f.src = src
	if f.Client == nil {
		f.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if f.Workers <= 0 {
		f.Workers = 3
	}
	f.lim = rate.NewLimiter(rate.Every(200*time.Millisecond), 1)

	var dbCodes []string
	cr, err := f.Pool.Query(ctx, `SELECT code FROM finish_codes WHERE brand_code=$1`, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	for cr.Next() {
		var c string
		if err := cr.Scan(&c); err != nil {
			cr.Close()
			return 0, 0, err
		}
		dbCodes = append(dbCodes, c)
	}
	cr.Close()
	f.dbCodes = dbCodes
	if err := f.src.init(ctx, f); err != nil {
		return 0, 0, err
	}

	limit := ""
	if f.Limit > 0 {
		limit = " LIMIT " + strconv.Itoa(f.Limit)
	}
	rows, err := f.Pool.Query(ctx, `
		SELECT i.id, i.family_id, i.sku,
		       COALESCE((SELECT array_agg(a.origin->>'url') FROM asset_links al JOIN assets a ON a.id=al.asset_id
		                 WHERE al.item_id=i.id AND a.source='vendor' AND a.origin ? 'url'), '{}')
		FROM items i JOIN families f ON f.id=i.family_id JOIN brands b ON b.id=f.brand_id
		WHERE b.code=$1 AND f.tier < 3
		ORDER BY i.family_id, i.sku`+limit, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.itemID, &t.familyID, &t.sku, &t.have); err != nil {
			rows.Close()
			return 0, 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()

	// misses from earlier runs
	missed := map[string]bool{}
	mr, err := f.Pool.Query(ctx, `SELECT url FROM vendor_lookups WHERE brand_code=$1 AND NOT found`, f.Brand)
	if err != nil {
		return 0, 0, err
	}
	for mr.Next() {
		var u string
		if err := mr.Scan(&u); err == nil {
			missed[u] = true
		}
	}
	mr.Close()

	var mu sync.Mutex
	cache := map[string]int64{} // url → asset id (0 = miss) within this run
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(f.Workers)
	start := time.Now()
	done := 0
	for _, t := range targets {
		t := t
		code, urls := f.src.candidates(t.sku)
		if code == "" {
			continue
		}
		if hasCode(t.have, code) {
			continue // the real photo of this finish is already linked
		}
		var fresh []string
		for _, u := range urls {
			if !missed[u] {
				fresh = append(fresh, u)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		mu.Lock()
		tried++
		mu.Unlock()
		g.Go(func() error {
			aid, hit, err := f.resolve(gctx, fresh, t, code, &mu, cache)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			done++
			if aid != 0 {
				found++
			}
			if done%100 == 0 {
				slog.Info("vendors fetch", "brand", f.Brand, "found", found, "tried", done, "of", tried, "last", hit)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return found, tried, err
	}
	slog.Info("vendors fetch done", "brand", f.Brand, "found", found, "tried", tried, "took", time.Since(start).Round(time.Second))
	return found, tried, nil
}

func hasCode(urls []string, code string) bool {
	for _, u := range urls {
		b := strings.ToUpper(strings.TrimSuffix(path.Base(u), path.Ext(u)))
		if strings.HasSuffix(b, "-"+code) {
			return true
		}
	}
	return false
}

// resolve tries the candidate URLs for one SKU, stores the first real image as an asset and
// links it to the item. The url→asset cache lets SKUs that share a picture share the row.
func (f *Fetcher) resolve(ctx context.Context, urls []string, t target, code string, mu *sync.Mutex, cache map[string]int64) (int64, string, error) {
	for _, u := range urls {
		mu.Lock()
		aid, seen := cache[u]
		mu.Unlock()
		if !seen {
			body, ok, err := f.get(ctx, u)
			if err != nil {
				return 0, "", err
			}
			if ok && !f.src.valid(body) {
				ok = false
			}
			if ok && !f.DryRun {
				aid, err = f.store(ctx, u, body, t, code)
				if err != nil {
					return 0, "", err
				}
			} else if ok {
				aid = -1 // dry run: counts as found, nothing stored
				slog.Info("would store", "sku", t.sku, "url", u, "bytes", len(body))
			}
			if !f.DryRun {
				f.Pool.Exec(ctx, `INSERT INTO vendor_lookups (brand_code, url, found) VALUES ($1,$2,$3) ON CONFLICT (brand_code, url) DO UPDATE SET found=EXCLUDED.found, tried_at=now()`, f.Brand, u, ok)
			}
			mu.Lock()
			cache[u] = aid
			mu.Unlock()
		}
		if aid != 0 {
			if aid > 0 && !f.DryRun {
				if _, err := f.Pool.Exec(ctx, `INSERT INTO asset_links (asset_id, family_id, item_id, how, weight) VALUES ($1,$2,$3,'vendor',1) ON CONFLICT DO NOTHING`, aid, t.familyID, t.itemID); err != nil {
					return 0, "", err
				}
			}
			return aid, u, nil
		}
	}
	return 0, "", nil
}

func (f *Fetcher) get(ctx context.Context, u string) ([]byte, bool, error) {
	if err := f.lim.Wait(ctx); err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, nil
	}
	req.Header.Set("User-Agent", "GeorgesShowroom/2 (dealer image sync; +https://georgesshowroom.com)")
	req.Header.Set("Accept", "image/jpeg,image/png,image/*;q=0.8")
	resp, err := f.Client.Do(req)
	if err != nil {
		slog.Warn("vendors fetch", "url", u, "err", err)
		return nil, false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil || len(body) < 2048 {
		return nil, false, nil
	}
	return body, true, nil
}

// getAny fetches a small non-image resource (a data file) under the same rate limit.
func (f *Fetcher) getAny(ctx context.Context, u string) ([]byte, bool, error) {
	if err := f.lim.Wait(ctx); err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, nil
	}
	req.Header.Set("User-Agent", "GeorgesShowroom/2 (dealer image sync; +https://georgesshowroom.com)")
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return body, err == nil, nil
}

// store writes the bytes under webimg/<BRAND>/<file> and upserts the asset row.
func (f *Fetcher) store(ctx context.Context, u string, body []byte, t target, code string) (int64, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return 0, nil // not an image we can read: treat as a miss
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	rel := filepath.ToSlash(filepath.Join("webimg", f.Brand, f.src.file(u, t.sku, code)))
	abs := filepath.Join(f.DataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, err
	}
	if st, err := os.Stat(abs); err != nil || st.Size() != int64(len(body)) {
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return 0, err
		}
	}
	origin, _ := json.Marshal(map[string]any{"url": u, "sku": t.sku, "finish_code": code, "w": cfg.Width, "h": cfg.Height})
	var id int64
	err = f.Pool.QueryRow(ctx, `
		INSERT INTO assets (hash, src_path, format, width, height, bytes, source, origin)
		VALUES ($1,$2,$3,$4,$5,$6,'vendor',$7)
		ON CONFLICT (hash) DO UPDATE SET origin = assets.origin || EXCLUDED.origin
		RETURNING id`, hash, rel, format, cfg.Width, cfg.Height, len(body), string(origin)).Scan(&id)
	return id, err
}
