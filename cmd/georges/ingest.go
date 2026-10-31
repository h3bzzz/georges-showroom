package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/site"
	"georges_/internal/store"
)

func ingestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ingest", Short: "Load crawled data into Postgres"}
	brands := &cobra.Command{
		Use:   "brands [data/brands.tsv]",
		Short: "Load the brand table (code, slug, name)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "data/brands.tsv"
			if len(args) == 1 {
				path = args[0]
			}
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			n := 0
			for sc.Scan() {
				parts := strings.Split(sc.Text(), "\t")
				if len(parts) < 3 || parts[1] == "slug" {
					continue
				}
				logo := ""
				if parts[0] != "" {
					logo = site.BaseURL + "/" + parts[0] + "-" + strings.ReplaceAll(parts[1], " ", "-") + ".Gif"
				}
				if _, err := st.UpsertBrand(cmd.Context(), parts[0], parts[1], parts[2], logo); err != nil {
					return fmt.Errorf("%s: %w", parts[1], err)
				}
				n++
			}
			fmt.Printf("upserted %d brands\n", n)
			return nil
		},
	}
	products := &cobra.Command{
		Use:   "products [DATA_DIR/products]",
		Short: "Load product JSON files written by `crawl product` / `crawl listing --follow`",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			dir := filepath.Join(cfg.DataDir, "products")
			if len(args) == 1 {
				dir = args[0]
			}
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, bad := 0, 0
			err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
					return err
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var p site.Product
				if err := json.Unmarshal(b, &p); err != nil || p.SKU == "" {
					bad++
					return nil
				}
				if p.LocalImages == nil {
					p.LocalImages = map[string]string{}
				}
				for _, img := range p.Images {
					rel := imageRelPath(img)
					if _, err := os.Stat(filepath.Join(cfg.DataDir, rel)); err == nil {
						p.LocalImages[img] = filepath.ToSlash(rel)
					}
				}
				if _, err := st.UpsertProduct(cmd.Context(), &p); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				n++
				if n%500 == 0 {
					fmt.Printf("  %d products\n", n)
				}
				return nil
			})
			fmt.Printf("ingested %d products (%d skipped)\n", n, bad)
			return err
		},
	}
	rooms := &cobra.Command{
		Use:   "rooms",
		Short: "Recompute the room (bathroom/kitchen/hardware/appliances) of every product family",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.ReassignRooms(cmd.Context())
			if err != nil {
				return err
			}
			counts, _ := st.RoomCounts(cmd.Context())
			fmt.Printf("reassigned %d families: %v\n", n, counts)
			return nil
		},
	}
	link := &cobra.Command{
		Use:   "link",
		Short: "Rebuild family ↔ catalog-crop links (exact SKU, family key, SKU prefix)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.LinkCrops(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("%d family↔crop links\n", n)
			return nil
		},
	}
	best := &cobra.Command{
		Use:   "best",
		Short: "Re-pick each family's feature photo/drawing/page from existing links (no relink)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			return st.RefreshFamilyBest(cmd.Context())
		},
	}
	var rtBrand string
	var rtLayout, rtDry bool
	var rtShow int
	retitle := &cobra.Command{
		Use:   "retitle",
		Short: "Re-derive catalog family titles from the best price-book line (or a sane section heading); --layout uses the page layout around each SKU",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			if rtLayout {
				changes, err := st.RetitleFromLayout(cmd.Context(), cfg.DataDir, rtBrand, rtDry)
				if err != nil {
					return err
				}
				perBrand := map[string]int{}
				for i, c := range changes {
					perBrand[c.Brand]++
					if i < rtShow {
						fmt.Printf("%-18s %-45s → %s\n", c.Brand, trunc(c.Old, 45), c.New)
					}
				}
				var brands []string
				for b := range perBrand {
					brands = append(brands, b)
				}
				sort.Strings(brands)
				for _, b := range brands {
					fmt.Printf("  %-24s %d\n", b, perBrand[b])
				}
				if rtDry {
					fmt.Printf("dry run: %d families would be retitled\n", len(changes))
				} else {
					fmt.Printf("retitled %d families from page layout\n", len(changes))
				}
				return nil
			}
			n, err := st.RetitleCatalogFamilies(cmd.Context(), rtBrand)
			if err != nil {
				return err
			}
			fmt.Printf("retitled %d families\n", n)
			return nil
		},
	}
	retitle.Flags().StringVar(&rtBrand, "brand", "", "only this brand code")
	retitle.Flags().BoolVar(&rtLayout, "layout", false, "use OCR word positions: the header/description line above each SKU")
	retitle.Flags().BoolVar(&rtDry, "dry", false, "print proposed changes without saving")
	retitle.Flags().IntVar(&rtShow, "show", 60, "how many changes to print")
	var rfBrand string
	refamily := &cobra.Command{
		Use:   "refamily",
		Short: "Merge finish-variant SKUs (62858LF-PC/-PN/-SS) into one family per model",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			moved, dropped, err := st.RefamilyCatalogProducts(cmd.Context(), rfBrand)
			if err != nil {
				return err
			}
			fmt.Printf("moved %d products, dropped %d empty families\n", moved, dropped)
			return nil
		},
	}
	refamily.Flags().StringVar(&rfBrand, "brand", "", "only this brand code")
	var prBrand string
	var prAudit, prReset bool
	prices := &cobra.Command{
		Use:   "prices",
		Short: "Re-validate price-book prices, re-price catalog products and quarantine outliers (--audit only lists them)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			ctx := cmd.Context()
			if prReset {
				n, err := st.ResetPriceFlags(ctx, prBrand)
				if err != nil {
					return err
				}
				fmt.Printf("price flags cleared: %d (prices come back from the price books below)\n", n)
			}
			if !prAudit {
				cleared, err := st.RevalidateItemPrices(ctx, prBrand)
				if err != nil {
					return err
				}
				fmt.Printf("price-book lines: %d prices cleared (page numbers, index references, SKUs read as money)\n", cleared)
				n, err := st.PriceProductsFromCatalogs(ctx, prBrand)
				if err != nil {
					return err
				}
				fmt.Printf("products repriced: %d\n", n)
			}
			out, err := st.AuditPrices(ctx, prBrand)
			if err != nil {
				return err
			}
			for i, o := range out {
				if i >= 40 {
					fmt.Printf("… %d more\n", len(out)-i)
					break
				}
				fmt.Printf("  %-18s %-14s %12s  fence %s–%s  %s  %s\n", o.Brand, o.SKU, money(o.PriceCents), money(o.Lo), money(o.Hi), o.Reason, o.Title)
			}
			if prAudit {
				fmt.Printf("%d outliers (run without --audit to quarantine)\n", len(out))
				return nil
			}
			q, err := st.QuarantinePrices(ctx, out)
			if err != nil {
				return err
			}
			fmt.Printf("quarantined %d outliers\n", q)
			stt, err := st.PriceStats(ctx, prBrand)
			if err != nil {
				return err
			}
			fmt.Printf("catalog products: %d priced, %d flagged, %d unpriced; median %s, p99 %s, max %s\n",
				stt.Priced, stt.Flagged, stt.Unpriced, money(stt.Median), money(stt.P99), money(stt.Max))
			return nil
		},
	}
	prices.Flags().StringVar(&prBrand, "brand", "", "only this brand code")
	prices.Flags().BoolVar(&prAudit, "audit", false, "list outliers without changing anything")
	prices.Flags().BoolVar(&prReset, "reset", false, "clear earlier quarantine flags first, so the fence is applied afresh")
	var catForce bool
	categories := &cobra.Command{
		Use:   "categories",
		Short: "Give catalog families a merchandised category from their titles (sidebar facets); --force reassigns",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.AssignCategories(cmd.Context(), catForce)
			if err != nil {
				return err
			}
			fmt.Printf("categories set on %d families\n", n)
			return nil
		},
	}
	categories.Flags().BoolVar(&catForce, "force", false, "reassign families that already have a category")
	var fnBrand string
	finishes := &cobra.Command{
		Use:   "finishes",
		Short: "Fill products.finish for catalog SKUs from their suffix code (finish_codes table), then refresh the search blob",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.ApplyFinishCodes(cmd.Context(), fnBrand)
			if err != nil {
				return err
			}
			fmt.Printf("finish set on %d products\n", n)
			return nil
		},
	}
	finishes.Flags().StringVar(&fnBrand, "brand", "", "only this brand code")
	cmd.AddCommand(brands, products, rooms, link, best, retitle, refamily, finishes, prices, categories)
	return cmd
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// money formats cents as "$1,234.56" for command output.
func money(c int64) string {
	neg := ""
	if c < 0 {
		neg, c = "-", -c
	}
	d := fmt.Sprintf("%d", c/100)
	for i := len(d) - 3; i > 0; i -= 3 {
		d = d[:i] + "," + d[i:]
	}
	return fmt.Sprintf("%s$%s.%02d", neg, d, c%100)
}
