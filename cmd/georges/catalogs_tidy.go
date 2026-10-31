package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"georges_/internal/extract"
	"georges_/internal/store"
)

// catalogsTidyCmd re-cuts photo crops so that slivers of the neighbouring picture and the
// caption under the photo are gone. The crop file (and its upscaled crops_hd twin, scaled) is
// rewritten in place and the stored width/height updated; crops are derived artefacts that
// `catalogs extract` can regenerate.
func catalogsTidyCmd() *cobra.Command {
	var brand string
	var dry bool
	var limit int
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Trim neighbour slivers and captions off photo crops (rewrites crop files in place)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := cmd.Context()
			rows, err := st.Pool.Query(ctx, `
				SELECT cc.id, cc.path, COALESCE(cc.hd_path,''), cc.w, cc.h
				FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
				WHERE cc.kind='photo' AND ($1='' OR c.brand_code=$1) ORDER BY cc.id LIMIT $2`, brand, limit)
			if err != nil {
				return err
			}
			type crop struct {
				id       int64
				path, hd string
				w, h     int
			}
			crops, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (crop, error) {
				var c crop
				err := r.Scan(&c.id, &c.path, &c.hd, &c.w, &c.h)
				return c, err
			})
			if err != nil {
				return err
			}
			fmt.Printf("%d photo crops\n", len(crops))
			start := time.Now()
			changed, failed := 0, 0
			for i, c := range crops {
				full := filepath.Join(cfg.DataDir, filepath.FromSlash(c.path))
				img, err := decode(full)
				if err != nil {
					failed++
					continue
				}
				keep, ok := extract.Tidy(img)
				if !ok {
					continue
				}
				changed++
				if dry {
					if changed <= 40 {
						fmt.Printf("  %s %dx%d → %v\n", c.path, img.Bounds().Dx(), img.Bounds().Dy(), keep)
					}
					continue
				}
				if err := writeSub(img, keep, full); err != nil {
					failed++
					continue
				}
				if c.hd != "" {
					hdFull := filepath.Join(cfg.DataDir, filepath.FromSlash(c.hd))
					if hd, err := decode(hdFull); err == nil {
						sx := float64(hd.Bounds().Dx()) / float64(img.Bounds().Dx())
						sy := float64(hd.Bounds().Dy()) / float64(img.Bounds().Dy())
						hk := image.Rect(int(float64(keep.Min.X)*sx), int(float64(keep.Min.Y)*sy), int(float64(keep.Max.X)*sx), int(float64(keep.Max.Y)*sy)).Intersect(hd.Bounds())
						_ = writeSub(hd, hk, hdFull)
						_, _ = st.Pool.Exec(ctx, `UPDATE catalog_crops SET hd_w=$2 WHERE id=$1`, c.id, hk.Dx())
					}
				}
				_, _ = st.Pool.Exec(ctx, `UPDATE catalog_crops SET w=$2, h=$3 WHERE id=$1`, c.id, keep.Dx(), keep.Dy())
				if (i+1)%2000 == 0 {
					fmt.Printf("  %d/%d  %s\n", i+1, len(crops), time.Since(start).Round(time.Second))
				}
			}
			fmt.Printf("done in %s: %d trimmed, %d unreadable\n", time.Since(start).Round(time.Second), changed, failed)
			if !dry && changed > 0 {
				fmt.Println("re-picking family photos…")
				return st.RefreshFamilyBest(ctx)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().BoolVar(&dry, "dry", false, "report what would change")
	cmd.Flags().IntVar(&limit, "limit", 1000000, "max crops")
	return cmd
}

func decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func writeSub(img image.Image, r image.Rectangle, path string) error {
	sub := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(sub, sub.Bounds(), img, r.Min, draw.Src)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, sub, &jpeg.Options{Quality: 92}); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}
