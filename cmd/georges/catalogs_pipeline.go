package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/catalogs"
	"georges_/internal/extract"
	"georges_/internal/ocr"
	"georges_/internal/store"
	"georges_/internal/toc"
)

// catalogsPipelineCmd runs download → OCR → extract for each catalog in turn, so a multi-hour
// run over the whole library is resumable and produces usable data as it goes.
func catalogsPipelineCmd() *cobra.Command {
	var brand string
	var workers int
	var rps float64
	var relink, classify bool
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "For every catalog: download pages, OCR, extract product images (resumable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.ListCatalogs(cmd.Context(), brand)
			if err != nil {
				return err
			}
			cl := catalogs.NewClient(rps)
			dir := filepath.Join(cfg.DataDir, "catalogs")
			opts := extract.Defaults()
			start := time.Now()
			for i, c := range rows {
				if c.Pages == 0 {
					continue
				}
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				b := catalogs.Book{Code: c.BrandCode, Name: c.Name}
				t0 := time.Now()
				// 1. pages
				if err := cl.FetchPages(cmd.Context(), b, 1, c.Pages, dir, workers, nil); err != nil {
					fmt.Printf("FAIL fetch %s: %v\n", b, err)
					continue
				}
				// 2. OCR (pages not yet done)
				pages, err := st.PagesNeedingOCR(cmd.Context(), c.BrandCode, 100000)
				if err != nil {
					return err
				}
				ocrN := 0
				sem := make(chan struct{}, workers)
				done := make(chan error, len(pages))
				for _, p := range pages {
					if p.CatalogID != c.ID {
						continue
					}
					ocrN++
					sem <- struct{}{}
					go func(p store.PageRef) {
						defer func() { <-sem }()
						done <- ocrPage(cmd, st, cfg.DataDir, p)
					}(p)
				}
				for j := 0; j < ocrN; j++ {
					if err := <-done; err != nil {
						fmt.Printf("  ocr: %v\n", err)
					}
				}
				// 3. extract
				todo, err := st.PagesNeedingExtract(cmd.Context(), c.BrandCode, 100000)
				if err != nil {
					return err
				}
				crops, linked := 0, 0
				for _, p := range todo {
					if p.CatalogID != c.ID {
						continue
					}
					n, l, err := extractPage(cmd.Context(), st, cfg.DataDir, p, opts)
					if err != nil {
						fmt.Printf("  extract p%d: %v\n", p.Page, err)
						continue
					}
					crops += n
					linked += l
				}
				_, _ = st.UpsertCatalog(cmd.Context(), c.BrandCode, c.Name, c.Title, c.Pages, "", c.HasText, true)
				buildTOCAndItems(cmd, st, cfg.DataDir, c)
				// 4. classify the new crops (pixel audit, then CLIP) so a dimension drawing never
				// becomes a card photo for a freshly ingested brand; both only touch unscored crops
				if classify && crops > 0 {
					classifyBrand(cmd, c.BrandCode, workers)
				}
				if relink && (i+1)%3 == 0 {
					if n, err := st.LinkCrops(cmd.Context()); err == nil {
						fmt.Printf("      relinked: %d family↔crop links\n", n)
					}
				}
				fmt.Printf("[%d/%d] %-52s %4d pp  ocr=%-4d crops=%-5d linked=%-5d %s (total %s)\n",
					i+1, len(rows), b, c.Pages, ocrN, crops, linked, time.Since(t0).Round(time.Second), time.Since(start).Round(time.Minute))
			}
			if !relink {
				fmt.Println("done; run `georges ingest link` then `georges ingest best` to relink crops across all books")
				return nil
			}
			n, err := st.LinkCrops(cmd.Context())
			fmt.Printf("final relink: %d family↔crop links (%v)\n", n, err)
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().IntVar(&workers, "workers", 2, "parallel downloads / OCR processes")
	cmd.Flags().Float64Var(&rps, "rps", 6, "download requests per second cap")
	cmd.Flags().BoolVar(&classify, "classify", true, "after each book run `catalogs audit` and `catalogs vision` for the brand (new crops only)")
	cmd.Flags().BoolVar(&relink, "relink", false, "run the global crop relink every 3 books and at the end (30+ min each on a full library; default: run `ingest link` once afterwards)")
	return cmd
}

func ocrPage(cmd *cobra.Command, st *store.Store, dataDir string, p store.PageRef) error {
	path := pageImagePath(dataDir, p)
	if _, err := os.Stat(path); err != nil {
		return nil // not downloaded (fetch failed); skip quietly
	}
	res, err := ocr.Run(cmd.Context(), path)
	if err != nil {
		return err
	}
	if b, err := jsonMarshal(res.Words); err == nil {
		_ = os.WriteFile(pathNoExt(path)+".words.json", b, 0o644)
	}
	var boxes []store.SKUBox
	for _, w := range res.Words {
		if ocr.LooksLikeSKU(w.Text) && w.Conf >= 50 {
			boxes = append(boxes, store.SKUBox{SKU: ocr.Normalize(w.Text), X: w.X, Y: w.Y, W: w.W, H: w.H, Conf: w.Conf})
		}
	}
	return st.SaveOCR(cmd.Context(), p.CatalogID, p.Page, path, res.Text, boxes)
}

// buildTOCAndItems derives the table of contents and price-book items for one catalog.
func buildTOCAndItems(cmd *cobra.Command, st *store.Store, dataDir string, c store.CatalogRow) {
	refs, err := st.OCRPages(cmd.Context(), c.BrandCode)
	if err != nil {
		return
	}
	heads := map[int]string{}
	pageWords := map[int][]ocr.Word{}
	canon := ocr.NewCanon()
	for _, p := range refs {
		if p.CatalogID != c.ID {
			continue
		}
		path := pageImagePath(dataDir, p)
		b, err := os.ReadFile(strings.TrimSuffix(path, ".jpg") + ".words.json")
		if err != nil {
			continue
		}
		var words []ocr.Word
		if json.Unmarshal(b, &words) != nil {
			continue
		}
		pageWords[p.Page] = words
		canon.Add(p.Page, words)
	}
	for _, p := range refs {
		words, ok := pageWords[p.Page]
		if p.CatalogID != c.ID || !ok {
			continue
		}
		heads[p.Page] = toc.PageHeading(words, 2200)
		var rows []store.ItemRow
		for _, ln := range ocr.Lines(words) {
			for _, k := range ln.SKUs {
				sku, ok := canon.Resolve(p.Page, k)
				if !ok {
					continue
				}
				rows = append(rows, store.ItemRow{CatalogID: p.CatalogID, Page: p.Page, SKU: sku, Line: ln.Text, PriceCents: ln.PriceCents, X: k.X, Y: k.Y})
			}
		}
		_ = st.SaveItems(cmd.Context(), p.CatalogID, p.Page, rows)
	}
	var entries []store.TOCEntry
	for _, e := range toc.Fold(heads, c.Pages) {
		entries = append(entries, store.TOCEntry{Page: e.Page, Title: e.Title, Level: e.Level})
	}
	_ = st.ReplaceTOC(cmd.Context(), c.ID, entries, "ocr")
	if n, err := st.ProductsFromCatalogs(cmd.Context(), c.BrandCode); err == nil && n > 0 {
		fmt.Printf("      +%d products from %s\n", n, c.Name)
	}
	_, _ = st.PriceProductsFromCatalogs(cmd.Context(), c.BrandCode)
	if out, err := st.AuditPrices(cmd.Context(), c.BrandCode); err == nil && len(out) > 0 {
		_, _ = st.QuarantinePrices(cmd.Context(), out)
	}
	if n, err := st.RetitleCatalogFamilies(cmd.Context(), c.BrandCode); err == nil && n > 0 {
		fmt.Printf("      retitled %d families\n", n)
	}
}

// classifyBrand runs the crop audit and the vision classifier for one brand in child processes
// of this same binary, so the pipeline needs no knowledge of either command's internals.
func classifyBrand(cmd *cobra.Command, code string, threads int) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	for _, args := range [][]string{
		{"catalogs", "audit", "--brand", code},
		{"catalogs", "vision", "--brand", code, "--threads", fmt.Sprint(max(threads, 1))},
	} {
		c := exec.CommandContext(cmd.Context(), self, args...)
		out, err := c.CombinedOutput()
		last := ""
		if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) > 0 {
			last = lines[len(lines)-1]
		}
		if err != nil {
			fmt.Printf("      %s %s: %v: %s\n", args[1], code, err, last)
			continue
		}
		fmt.Printf("      %s %s: %s\n", args[1], code, last)
	}
}
