package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/extract"
	"georges_/internal/ocr"
	"georges_/internal/store"
)

func pageImagePath(dataDir string, r store.PageRef) string {
	return filepath.Join(dataDir, "catalogs", r.BrandCode, safeName(r.Name), fmt.Sprintf("%04d.jpg", r.Page))
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

func catalogsExtractCmds() []*cobra.Command {
	var brand string
	var limit, workers int
	var force, sparse bool

	ocrCmd := &cobra.Command{
		Use:   "ocr",
		Short: "OCR downloaded catalog pages with tesseract; store text and SKU word boxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			list := st.PagesNeedingOCR
			if sparse {
				list = st.PagesWithSparseOCR
			}
			pages, err := list(cmd.Context(), brand, limit)
			if err != nil {
				return err
			}
			var todo []store.PageRef
			for _, p := range pages {
				if _, err := os.Stat(pageImagePath(cfg.DataDir, p)); err == nil {
					todo = append(todo, p)
				}
			}
			fmt.Printf("%d pages to OCR (%d pending overall, run `catalogs fetch-all` for the rest)\n", len(todo), len(pages))
			start := time.Now()
			var mu sync.Mutex
			done, skus := 0, 0
			jobs := make(chan store.PageRef)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for p := range jobs {
						path := pageImagePath(cfg.DataDir, p)
						res, err := ocr.Run(cmd.Context(), path)
						if err != nil {
							fmt.Printf("FAIL %s/%s p%d: %v\n", p.BrandCode, p.Name, p.Page, err)
							continue
						}
						if b, err := json.Marshal(res.Words); err == nil {
							_ = os.WriteFile(strings.TrimSuffix(path, ".jpg")+".words.json", b, 0o644)
						}
						var boxes []store.SKUBox
						for _, w := range res.Words {
							if ocr.LooksLikeSKU(w.Text) && w.Conf >= 50 {
								boxes = append(boxes, store.SKUBox{SKU: ocr.Normalize(w.Text), X: w.X, Y: w.Y, W: w.W, H: w.H, Conf: w.Conf})
							}
						}
						if err := st.SaveOCR(cmd.Context(), p.CatalogID, p.Page, path, res.Text, boxes); err != nil {
							fmt.Printf("FAIL save %s p%d: %v\n", p.Name, p.Page, err)
							continue
						}
						if sparse && len(boxes) > 0 {
							_ = st.ResetExtracted(cmd.Context(), p.CatalogID, p.Page) // crops get their SKUs on re-extract
						}
						mu.Lock()
						done++
						skus += len(boxes)
						if done%20 == 0 {
							fmt.Printf("  %d/%d pages, %d sku tokens, %s\n", done, len(todo), skus, time.Since(start).Round(time.Second))
						}
						mu.Unlock()
					}
				}()
			}
			for _, p := range todo {
				select {
				case jobs <- p:
				case <-cmd.Context().Done():
					close(jobs)
					wg.Wait()
					return cmd.Context().Err()
				}
			}
			close(jobs)
			wg.Wait()
			fmt.Printf("OCR done: %d pages, %d sku tokens in %s\n", done, skus, time.Since(start).Round(time.Second))
			return nil
		},
	}

	extractCmd := &cobra.Command{
		Use:   "extract",
		Short: "Detect product photos on OCR'd pages, crop them, link to nearby SKUs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			if force {
				if err := st.ResetExtract(cmd.Context(), brand); err != nil {
					return err
				}
			}
			pages, err := st.PagesNeedingExtract(cmd.Context(), brand, limit)
			if err != nil {
				return err
			}
			fmt.Printf("%d pages to extract\n", len(pages))
			opts := extract.Defaults()
			start := time.Now()
			var mu sync.Mutex
			done, crops, linked := 0, 0, 0
			jobs := make(chan store.PageRef)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for p := range jobs {
						n, l, err := extractPage(cmd.Context(), st, cfg.DataDir, p, opts)
						if err != nil {
							fmt.Printf("FAIL %s/%s p%d: %v\n", p.BrandCode, p.Name, p.Page, err)
							continue
						}
						mu.Lock()
						done++
						crops += n
						linked += l
						if done%25 == 0 {
							fmt.Printf("  %d/%d pages, %d crops (%d linked), %s\n", done, len(pages), crops, linked, time.Since(start).Round(time.Second))
						}
						mu.Unlock()
					}
				}()
			}
			for _, p := range pages {
				select {
				case jobs <- p:
				case <-cmd.Context().Done():
					close(jobs)
					wg.Wait()
					return cmd.Context().Err()
				}
			}
			close(jobs)
			wg.Wait()
			fmt.Printf("extract done: %d pages, %d crops, %d linked to a SKU, %s\n", done, crops, linked, time.Since(start).Round(time.Second))
			return nil
		},
	}
	extractCmd.Flags().BoolVar(&force, "force", false, "re-extract pages already processed")
	ocrCmd.Flags().BoolVar(&sparse, "sparse", false, "re-read pages whose OCR came back nearly blank (pale hairline type) with image enhancement")
	for _, c := range []*cobra.Command{ocrCmd, extractCmd} {
		c.Flags().StringVar(&brand, "brand", "", "only this brand code")
		c.Flags().IntVar(&limit, "limit", 100000, "max pages this run")
		c.Flags().IntVar(&workers, "workers", 3, "parallel workers")
	}
	return []*cobra.Command{ocrCmd, extractCmd}
}

func extractPage(ctx context.Context, st *store.Store, dataDir string, p store.PageRef, opts extract.Options) (int, int, error) {
	path := pageImagePath(dataDir, p)
	img, err := extract.Load(path)
	if err != nil {
		return 0, 0, err
	}
	boxes, err := st.PageSKUs(ctx, p.CatalogID, p.Page)
	if err != nil {
		return 0, 0, err
	}
	// Full word boxes are cached by `catalogs ocr` next to the page image; fall back to the
	// stored SKU boxes if the cache is missing.
	var words []ocr.Word
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".jpg") + ".words.json"); err == nil {
		_ = json.Unmarshal(b, &words)
	}
	if len(words) == 0 {
		for _, b := range boxes {
			words = append(words, ocr.Word{Text: b.SKU, X: b.X, Y: b.Y, W: b.W, H: b.H, Conf: b.Conf})
		}
	} else {
		// re-derive SKU boxes with the current heuristic (the OCR run may predate it)
		var fresh []store.SKUBox
		for _, w := range words {
			if ocr.LooksLikeSKU(w.Text) && w.Conf >= 50 {
				fresh = append(fresh, store.SKUBox{SKU: ocr.Normalize(w.Text), X: w.X, Y: w.Y, W: w.W, H: w.H, Conf: w.Conf})
			}
		}
		_ = st.ReplacePageSKUs(ctx, p.CatalogID, p.Page, fresh)
	}
	regs := extract.Regions(img, words, opts)
	matches := extract.LinkSKUs(regs, words, opts)
	bySKU := map[extract.Rect]extract.Match{}
	for _, m := range matches {
		bySKU[m.Region.Rect] = m
	}
	outDir := filepath.Join(dataDir, "crops", p.BrandCode, safeName(p.Name))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, 0, err
	}
	var rows []store.CropRow
	linked := 0
	for i, r := range regs {
		if i >= 40 {
			break
		}
		name := fmt.Sprintf("%04d_%02d.jpg", p.Page, i)
		full := filepath.Join(outDir, name)
		rect := extract.TrimText(r.Rect, words, 4)
		if _, _, err := extract.Crop(img, rect, 6, full); err != nil {
			return 0, 0, err
		}
		row := store.CropRow{CatalogID: p.CatalogID, Page: p.Page, X: rect.X, Y: rect.Y, W: rect.W, H: rect.H, Kind: r.Kind,
			Path: filepath.ToSlash(filepath.Join("crops", p.BrandCode, safeName(p.Name), name))}
		if m, ok := bySKU[r.Rect]; ok {
			row.SKU, row.Score = m.SKU, m.Score
			linked++
		}
		rows = append(rows, row)
	}
	return len(rows), linked, st.SaveCrops(ctx, p.CatalogID, p.Page, rows)
}
