package main

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/extract"
	"georges_/internal/store"
	"georges_/internal/upscale"
)

// upscaleCmd super-resolves small catalog crops with Real-ESRGAN and swaps the HD copies into
// family_best as it goes. Resumable: only crops without hd_w are picked up.
func upscaleCmd() *cobra.Command {
	var brand, model string
	var limit, maxW, batch, gpu, quality int
	var all bool
	cmd := &cobra.Command{
		Use:   "upscale",
		Short: "Real-ESRGAN 4x for small catalog crops (best photo per family first); writes crops_hd/ and promotes them",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			bin, err := upscale.Ensure(ctx, cfg.DataDir)
			if err != nil {
				return err
			}
			if err := upscale.EnsureModel(ctx, bin, model); err != nil {
				return err
			}
			work := filepath.Join(cfg.DataDir, "tools", "upscale-work")
			_ = os.RemoveAll(work)
			done, failed, small, _ := st.HDStats(ctx)
			fmt.Printf("hd so far: %d done, %d failed, %d family photos still small · model=%s gpu=%d\n", done, failed, small, model, gpu)
			start, total := time.Now(), 0
			for limit <= 0 || total < limit {
				n := batch
				if limit > 0 && limit-total < n {
					n = limit - total
				}
				cands, err := st.CropsNeedingHD(ctx, brand, maxW, n, all)
				if err != nil {
					return err
				}
				if len(cands) == 0 {
					break
				}
				res, err := upscaleBatch(ctx, bin, cfg.DataDir, work, cands, upscale.Options{Model: model, GPU: gpu}, quality)
				if err != nil {
					return err
				}
				promoted, err := st.SetCropHD(ctx, res)
				if err != nil {
					return err
				}
				total += len(cands)
				ok := 0
				for _, r := range res {
					if r.W > 0 {
						ok++
					}
				}
				el := time.Since(start)
				fmt.Printf("%s  +%d (%d ok)  total=%d  promoted=%d  %.1fs/img\n",
					time.Now().Format("15:04:05"), len(cands), ok, total, promoted, el.Seconds()/float64(total))
			}
			_ = os.RemoveAll(work)
			done, failed, small, _ = st.HDStats(ctx)
			fmt.Printf("finished: %d this run in %s · hd total %d (%d failed), %d family photos still small\n",
				total, time.Since(start).Round(time.Second), done, failed, small)
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code (e.g. ABW)")
	cmd.Flags().StringVar(&model, "model", upscale.DefaultModel, "realesrgan-x4plus (default) | 4xNomos8kSC_fp16 (CC BY, auto-downloaded) | realesrgan-x4plus-anime")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many crops (0 = until none left)")
	cmd.Flags().IntVar(&maxW, "max-w", 500, "only crops narrower than this")
	cmd.Flags().IntVar(&batch, "batch", 24, "crops per ncnn process")
	cmd.Flags().IntVar(&gpu, "gpu", 0, "vulkan device index (-1 = CPU)")
	cmd.Flags().IntVar(&quality, "quality", 88, "JPEG quality for the HD copy")
	cmd.Flags().BoolVar(&all, "all", false, "every linked crop, not just the ones shown as a family's photo/drawing")
	return cmd
}

// upscaleBatch runs one ncnn process over cands and writes crops_hd/<same relative path>.
//
// The model's input is cut again from the full page render and handed over as PNG: the stored
// crop is a JPEG re-encode (4:2:0 chroma) of a JPEG, and feeding that second generation of
// artifacts into super-resolution is what produced colour fringing on metal finishes. Pages are
// decoded once per batch and cached by path.
func upscaleBatch(ctx context.Context, bin, dataDir, work string, cands []store.HDCandidate, o upscale.Options, quality int) ([]store.HDResult, error) {
	inDir, outDir := filepath.Join(work, "in"), filepath.Join(work, "out")
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(inDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	pages := map[string]image.Image{}
	for _, c := range cands {
		dst := filepath.Join(inDir, strconv.FormatInt(c.ID, 10))
		pagePath := pageImagePath(dataDir, store.PageRef{BrandCode: c.BrandCode, Name: c.Name, Page: c.Page})
		img, ok := pages[pagePath]
		if !ok {
			if img, _ = extract.Load(pagePath); img != nil {
				pages[pagePath] = img
			}
		}
		if img != nil {
			if err := writePNGCrop(img, extract.Rect{X: c.X, Y: c.Y, W: c.W, H: c.H}, cropMargin, dst+".png"); err == nil {
				continue
			}
		}
		// page render missing (not downloaded on this machine): fall back to the stored JPEG crop
		if err := copyFile(filepath.Join(dataDir, c.Path), dst+".jpg"); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", c.Path, err)
		}
	}
	if err := upscale.RunDir(ctx, bin, inDir, outDir, o); err != nil {
		return nil, err
	}
	res := make([]store.HDResult, 0, len(cands))
	for _, c := range cands {
		src := filepath.Join(outDir, strconv.FormatInt(c.ID, 10)+".png")
		rel := "crops_hd/" + strings.TrimPrefix(c.Path, "crops/")
		w, err := upscale.SaveJPEG(src, filepath.Join(dataDir, filepath.FromSlash(rel)), quality)
		if err != nil {
			res = append(res, store.HDResult{ID: c.ID, W: -1})
			continue
		}
		res = append(res, store.HDResult{ID: c.ID, Path: rel, W: w})
	}
	return res, nil
}

// cropMargin matches what `catalogs extract` adds around a region when it writes the crop file.
const cropMargin = 6

func writePNGCrop(img image.Image, r extract.Rect, margin int, path string) error {
	b := img.Bounds()
	x0, y0 := max(b.Min.X, r.X-margin), max(b.Min.Y, r.Y-margin)
	x1, y1 := min(b.Max.X, r.X+r.W+margin), min(b.Max.Y, r.Y+r.H+margin)
	if x1-x0 < 8 || y1-y0 < 8 {
		return fmt.Errorf("crop %v outside page", r)
	}
	sub := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(sub, sub.Bounds(), img, image.Pt(x0, y0), draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, sub); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func copyFile(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
