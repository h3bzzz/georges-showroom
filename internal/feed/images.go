package feed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jlaffaye/ftp"
)

// Images pulls the feed's own product shots (ftp images/<BRAND>/<file>) for families that have
// no picture at all and registers them as assets of source 'feed', linked to the SKU. They are
// the maker's small web images, so the picker ranks them under every catalog or vendor picture;
// they only stop a card from being blank.
type Images struct {
	Pool             *pgxpool.Pool
	DataDir          string
	Host, User, Pass string
	Brand            string
	Limit            int
	DryRun           bool
}

type imgTarget struct {
	productID int64
	brand     string
	file      string // basename on the ftp server
	itemID    int64
	familyID  int64
}

// Run downloads and registers; returns pictures stored.
func (im *Images) Run(ctx context.Context) (int, error) {
	q := `
		SELECT fp.product_id, fp.brand_code, fp.image_name, i.id, i.family_id
		FROM feed_products fp
		JOIN brands b ON b.code = fp.brand_code
		JOIN items i ON i.brand_id = b.id AND i.sku_norm = fp.sku_norm
		WHERE fp.image_name IS NOT NULL AND ($1 = '' OR fp.brand_code = $1)
		  AND NOT EXISTS (SELECT 1 FROM family_media fm WHERE fm.family_id = i.family_id AND fm.role = 'card')
		  AND NOT EXISTS (SELECT 1 FROM asset_links al JOIN assets a ON a.id = al.asset_id WHERE al.family_id = i.family_id AND a.source = 'feed')
		  AND NOT EXISTS (SELECT 1 FROM vendor_lookups vl WHERE vl.brand_code = fp.brand_code AND vl.url = 'feed:' || fp.image_name AND NOT vl.found)
		ORDER BY fp.brand_code, i.family_id, fp.product_id`
	if im.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", im.Limit)
	}
	rows, err := im.Pool.Query(ctx, q, im.Brand)
	if err != nil {
		return 0, err
	}
	var targets []imgTarget
	seenFam := map[int64]bool{}
	for rows.Next() {
		var t imgTarget
		var name string
		if err := rows.Scan(&t.productID, &t.brand, &name, &t.itemID, &t.familyID); err != nil {
			rows.Close()
			return 0, err
		}
		if seenFam[t.familyID] { // one picture per family is enough to fill the card
			continue
		}
		seenFam[t.familyID] = true
		t.file = path.Base(strings.ReplaceAll(name, "\\", "/"))
		if t.file == "" || t.file == "." {
			continue
		}
		targets = append(targets, t)
	}
	rows.Close()
	slog.Info("feed images", "families", len(targets), "brand", im.Brand)
	if im.DryRun || len(targets) == 0 {
		return 0, nil
	}
	conn, err := im.dial(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Quit()
	stored, misses := 0, 0
	t0 := time.Now()
	for i, t := range targets {
		if ctx.Err() != nil {
			break
		}
		rel := filepath.ToSlash(filepath.Join("feed", "img", t.brand, t.file))
		abs := filepath.Join(im.DataDir, filepath.FromSlash(rel))
		body, err := os.ReadFile(abs)
		if err != nil {
			body, err = im.fetch(ctx, &conn, "images/"+t.brand+"/"+t.file)
			if err != nil {
				misses++
				im.Pool.Exec(ctx, `INSERT INTO vendor_lookups (brand_code, url, found) VALUES ($1,$2,false) ON CONFLICT DO NOTHING`, t.brand, "feed:/"+t.brand+"/"+t.file)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return stored, err
			}
			if err := os.WriteFile(abs, body, 0o644); err != nil {
				return stored, err
			}
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || cfg.Width < 80 || cfg.Height < 80 {
			misses++
			os.Remove(abs)
			im.Pool.Exec(ctx, `INSERT INTO vendor_lookups (brand_code, url, found) VALUES ($1,$2,false) ON CONFLICT DO NOTHING`, t.brand, "feed:/"+t.brand+"/"+t.file)
			continue
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		origin, _ := json.Marshal(map[string]any{"url": "ftp:images/" + t.brand + "/" + t.file, "feed_product_id": t.productID})
		var aid int64
		if err := im.Pool.QueryRow(ctx, `
			INSERT INTO assets (hash, src_path, format, width, height, bytes, source, origin)
			VALUES ($1,$2,$3,$4,$5,$6,'feed',$7)
			ON CONFLICT (hash) DO UPDATE SET origin = assets.origin || EXCLUDED.origin
			RETURNING id`, hash, rel, format, cfg.Width, cfg.Height, len(body), string(origin)).Scan(&aid); err != nil {
			return stored, err
		}
		if _, err := im.Pool.Exec(ctx, `INSERT INTO asset_links (asset_id, family_id, item_id, how, weight) VALUES ($1,$2,$3,'feed',0.6) ON CONFLICT DO NOTHING`, aid, t.familyID, t.itemID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				// the family was re-derived while we were downloading; the file stays cached
				// and the next run links it
				misses++
				continue
			}
			return stored, err
		}
		stored++
		if (i+1)%200 == 0 {
			slog.Info("feed images", "done", i+1, "of", len(targets), "stored", stored, "misses", misses, "rate/s", fmt.Sprintf("%.1f", float64(i+1)/time.Since(t0).Seconds()))
		}
	}
	slog.Info("feed images done", "stored", stored, "misses", misses, "took", time.Since(t0).Round(time.Second))
	return stored, nil
}

func (im *Images) dial(ctx context.Context) (*ftp.ServerConn, error) {
	if im.User == "" || im.Pass == "" {
		return nil, fmt.Errorf("feed images: FTP_USER and FTP_PASS must be set")
	}
	host := im.Host
	if host == "" {
		host = "ftp.myplumbingshowroom.com"
	}
	conn, err := ftp.Dial(host+":21", ftp.DialWithContext(ctx), ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return nil, err
	}
	if err := conn.Login(im.User, im.Pass); err != nil {
		conn.Quit()
		return nil, err
	}
	return conn, nil
}

// fetch retrieves one file, reconnecting once when the control connection dropped.
func (im *Images) fetch(ctx context.Context, conn **ftp.ServerConn, remote string) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		res, err := (*conn).Retr(remote)
		if err != nil {
			if attempt == 0 && !isNotFound(err) {
				(*conn).Quit()
				c, derr := im.dial(ctx)
				if derr != nil {
					return nil, derr
				}
				*conn = c
				continue
			}
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(res, 20<<20))
		res.Close()
		if err != nil {
			return nil, err
		}
		if len(body) < 512 {
			return nil, fmt.Errorf("short file")
		}
		return body, nil
	}
	return nil, fmt.Errorf("fetch failed")
}

func isNotFound(err error) bool {
	s := err.Error()
	return strings.Contains(s, "550") || strings.Contains(s, "not found") || strings.Contains(s, "No such")
}
