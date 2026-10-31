package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"georges_/internal/extract"
	"georges_/internal/ocr"
	"georges_/internal/store"
)

// catalogsAuditCmd measures every photo crop (pixel stats + OCR words inside it) and moves
// tables, text blocks, blanks and line art out of the photo pool.
func catalogsAuditCmd() *cobra.Command {
	var brand string
	var dry, all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Classify photo crops (photo | drawing | table | text | blank) from pixel stats and OCR words; re-pick family photos",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.Pool.Query(ctx, `
				SELECT cc.id, cc.catalog_id, cc.page, cc.x, cc.y, cc.w, cc.h, cc.path, COALESCE(cc.pkind, cc.kind), c.brand_code, c.name
				FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
				WHERE ($1='' OR c.brand_code=$1) AND ($2 OR cc.quality IS NULL)
				ORDER BY cc.catalog_id, cc.page, cc.id`, brand, dry || all)
			if err != nil {
				return err
			}
			type crop struct {
				id                     int64
				cat, page, x, y, w, h  int
				path, kind, code, name string
			}
			var crops []crop
			for rows.Next() {
				var c crop
				if err := rows.Scan(&c.id, &c.cat, &c.page, &c.x, &c.y, &c.w, &c.h, &c.path, &c.kind, &c.code, &c.name); err != nil {
					rows.Close()
					return err
				}
				crops = append(crops, c)
				if limit > 0 && len(crops) >= limit {
					break
				}
			}
			rows.Close()
			fmt.Printf("%d crops to audit\n", len(crops))
			counts := map[string]int{}
			start := time.Now()
			var wordsCache []ocr.Word
			cacheKey := ""
			batch := &pgx.Batch{}
			flush := func() error {
				if batch.Len() == 0 {
					return nil
				}
				err := st.Pool.SendBatch(ctx, batch).Close()
				batch = &pgx.Batch{}
				return err
			}
			for i, c := range crops {
				// OCR words inside the crop
				key := fmt.Sprintf("%d/%d", c.cat, c.page)
				if key != cacheKey {
					cacheKey = key
					wordsCache = nil
					if b, err := os.ReadFile(filepath.Join(cfg.DataDir, "catalogs", c.code, safeName(c.name), fmt.Sprintf("%04d.words.json", c.page))); err == nil {
						_ = json.Unmarshal(b, &wordsCache)
					}
				}
				nWords, skuWords, dims, textArea := 0, 0, 0, 0.0
				mx, my := c.w/4, c.h/4 // dimension labels sit just outside the picture
				for _, w := range wordsCache {
					if w.Conf >= 25 && reDimLabel.MatchString(w.Text) {
						cx, cy := w.X+w.W/2, w.Y+w.H/2
						if cx >= c.x-mx && cx <= c.x+c.w+mx && cy >= c.y-my && cy <= c.y+c.h+my {
							dims++
						}
					}
					if w.Conf < 40 || len(w.Text) < 2 {
						continue
					}
					cx, cy := w.X+w.W/2, w.Y+w.H/2
					if cx >= c.x && cx <= c.x+c.w && cy >= c.y && cy <= c.y+c.h {
						nWords++
						textArea += float64(w.W * w.H)
						if ocr.LooksLikeSKU(w.Text) && w.Conf >= 60 {
							skuWords++
						}
					}
				}
				if c.w*c.h > 0 {
					textArea /= float64(c.w * c.h)
				}
				// pixels
				var ps extract.PixelStats
				if f, err := os.Open(filepath.Join(cfg.DataDir, filepath.FromSlash(c.path))); err == nil {
					if img, _, err := image.Decode(f); err == nil {
						step := 1
						if b := img.Bounds(); b.Dx()*b.Dy() > 250000 {
							step = 3
						} else if b.Dx()*b.Dy() > 60000 {
							step = 2
						}
						ps = extract.Stats(img, step)
					}
					f.Close()
				}
				kind := classify(c.kind, ps, nWords, skuWords, textArea)
				if kind == "photo" && dims >= 2 {
					kind = "drawing" // 7", 1-1/4", 8" around the picture: a dimensioned spec drawing
				}
				// quality: solid, shaded, not text; drawings score low so photos win
				q := ps.Ink * (0.4 + 0.6*(1-ps.Thin)) * (1 - min(1, textArea*3))
				if kind != "photo" {
					q *= 0.2
				}
				counts[kind]++
				if dry {
					if i < 60 || kind != c.kind {
						fmt.Printf("%-7s→%-7s ink=%.2f mid=%.2f light=%.2f sat=%.2f thin=%.2f blank=%.2f rules=%-2d asp=%.1f words=%-3d text=%.2f  %s\n", c.kind, kind, ps.Ink, ps.Midtone, ps.Light, ps.Sat, ps.Thin, ps.BlankRows, ps.RuleRows, ps.Aspect, nWords, textArea, c.path)
					}
					continue
				}
				batch.Queue(`UPDATE catalog_crops SET pkind=$2, kind=crop_kind($2, vkind, vscore), ink=$3, midtone=$4, sat=$5, words=$6, textarea=$7, quality=$8 WHERE id=$1`,
					c.id, kind, ps.Ink, ps.Midtone, ps.Sat, nWords, textArea, q)
				if batch.Len() >= 500 {
					if err := flush(); err != nil {
						return err
					}
				}
				if (i+1)%5000 == 0 {
					fmt.Printf("  %d/%d  %s\n", i+1, len(crops), time.Since(start).Round(time.Second))
				}
			}
			if err := flush(); err != nil {
				return err
			}
			fmt.Printf("done in %s: %v\n", time.Since(start).Round(time.Second), counts)
			if dry {
				return nil
			}
			fmt.Println("re-picking family photos…")
			if err := st.RefreshFamilyBest(ctx); err != nil {
				return err
			}
			_, err = st.PromoteHD(ctx)
			return err
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().BoolVar(&dry, "dry", false, "print classifications, change nothing")
	cmd.Flags().BoolVar(&all, "all", false, "re-audit crops that already have a quality score")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many crops")
	return cmd
}

// classify decides the crop kind from its statistics. Photos are solid objects (few ink pixels
// touch white); line art and text are thin; tables and text blocks have blank gaps between
// lines and, for tables, horizontal rules; blanks are empty boxes.
// reDimLabel: 7"  1-1/4"  3/8"  12mm  (9283) is not one
var reDimLabel = regexp.MustCompile(`^[\(\[]?\d{1,3}(-\d{1,2}/\d{1,2})?(/\d{1,2})?\s?("|”|″|''|in\.?|mm|cm)[\)\]]?$`)

func classify(cur string, ps extract.PixelStats, words, skuWords int, textArea float64) string {
	switch {
	case ps.Ink < 0.02:
		return "blank"
	case skuWords >= 1:
		return "table" // a product number printed inside the box: a price/spec table, never a photo
	case (words >= 10 && textArea >= 0.06) || textArea >= 0.15 || words >= 25:
		return "table"
	case ps.RuleRows >= 3 && ps.BlankRows >= 0.10 && ps.Thin >= 0.45:
		return "table" // ruled grid of small text
	case (words >= 3 && ps.RuleRows >= 2) || (words >= 4 && textArea >= 0.10):
		return "table" // a short SKU table (two or three ruled rows)
	case ps.Light >= 0.55 && ps.Midtone < 0.25:
		return "table" // light-gray shaded grid with marks in it (compatibility matrices)
	case ps.Aspect >= 2.5 && ps.Midtone < 0.30 && ps.Sat < 0.10:
		return "table" // a wide strip of black-on-white marks is a row of a table, not a product
	case words >= 1 && words <= 6 && textArea >= 0.25:
		return "text"
	case ps.BlankRows >= 0.30 && ps.Thin >= 0.55 && ps.Sat < 0.12:
		return "text" // lines of type with gaps between them (price columns, notes)
	case ps.Thin >= 0.62 && ps.Sat < 0.10:
		return "drawing" // black-on-white line art
	case ps.Sat < 0.05 && ps.Dark >= 0.45 && ps.Thin >= 0.35 && ps.Midtone < 0.45:
		// dimension sketch with thick black strokes (Gessi, Dornbracht spec art): the ink is
		// near-black with hardly any shading; a matte-black product is solid (thin ≈ 0.1–0.2)
		return "drawing"
	}
	return "photo" // solid, shaded or coloured object (dark products included)
}

var _ = context.Background
