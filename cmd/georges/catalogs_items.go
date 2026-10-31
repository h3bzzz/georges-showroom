package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/ocr"
	"georges_/internal/store"
)

func catalogsItemsCmd() *cobra.Command {
	var brand string
	var noLink bool
	items := &cobra.Command{
		Use:   "items",
		Short: "Turn OCR'd price-book lines into catalog_items (SKU, description, price) and create products for new SKUs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			refs, err := st.OCRPages(cmd.Context(), brand)
			if err != nil {
				return err
			}
			total, lines := 0, 0
			// pass 1: learn each book's trusted SKUs so mangled reads can be repaired
			type pw struct {
				ref   store.PageRef
				words []ocr.Word
			}
			var pages []pw
			canons := map[int]*ocr.Canon{}
			withCents, whole := map[int]int{}, map[int]int{}
			for _, p := range refs {
				path := pageImagePath(cfg.DataDir, p)
				b, err := os.ReadFile(strings.TrimSuffix(path, ".jpg") + ".words.json")
				if err != nil {
					continue
				}
				var words []ocr.Word
				if json.Unmarshal(b, &words) != nil {
					continue
				}
				if canons[p.CatalogID] == nil {
					canons[p.CatalogID] = ocr.NewCanon()
				}
				canons[p.CatalogID].Add(p.Page, words)
				c, h := ocr.CentsCounts(words)
				withCents[p.CatalogID] += c
				whole[p.CatalogID] += h
				pages = append(pages, pw{p, words})
			}
			for _, pg := range pages {
				p, words, canon := pg.ref, pg.words, canons[pg.ref.CatalogID]
				// a book that prints cents everywhere (Alno, Chicago Faucets) is read as cents on
				// every page, even one where OCR dropped most of the points
				bookCents := withCents[p.CatalogID] >= 20 && 2*withCents[p.CatalogID] >= whole[p.CatalogID]
				var rows []store.ItemRow
				for _, ln := range ocr.LinesCents(words, bookCents) {
					for _, k := range ln.SKUs {
						sku, ok := canon.Resolve(p.Page, k)
						if !ok || ocr.JunkSKU(sku) {
							continue
						}
						rows = append(rows, store.ItemRow{CatalogID: p.CatalogID, Page: p.Page, SKU: sku, Line: ln.Text, PriceCents: ln.PriceCents, X: k.X, Y: k.Y})
					}
				}
				if err := st.SaveItems(cmd.Context(), p.CatalogID, p.Page, rows); err != nil {
					return err
				}
				total++
				lines += len(rows)
			}
			fmt.Printf("items: %d pages scanned, %d SKU lines\n", total, lines)
			if jl, jp, err := st.PurgeJunkSKUs(cmd.Context(), brand, ocr.JunkSKU); err != nil {
				return err
			} else if jl+jp > 0 {
				fmt.Printf("purged %d junk-SKU lines and %d products (flow rates, standards, labels)\n", jl, jp)
			}
			n, err := st.ProductsFromCatalogs(cmd.Context(), brand)
			if err != nil {
				return err
			}
			fmt.Printf("created %d products from price books\n", n)
			if np, err := st.PriceProductsFromCatalogs(cmd.Context(), brand); err == nil {
				fmt.Printf("priced %d products from price books\n", np)
			}
			if out, err := st.AuditPrices(cmd.Context(), brand); err == nil && len(out) > 0 {
				q, _ := st.QuarantinePrices(cmd.Context(), out)
				fmt.Printf("quarantined %d implausible prices\n", q)
			}
			if rt, err := st.RetitleCatalogFamilies(cmd.Context(), brand); err == nil {
				fmt.Printf("retitled %d families from section headings\n", rt)
			}
			if n2, err := st.ReassignRooms(cmd.Context()); err == nil {
				fmt.Printf("rooms reassigned for %d families\n", n2)
			}
			if noLink {
				fmt.Println("skipped crop relink (--no-link); run `georges ingest link` after refamily")
				return nil
			}
			if links, err := st.LinkCrops(cmd.Context()); err == nil {
				fmt.Printf("%d family↔crop links\n", links)
			}
			return nil
		},
	}
	items.Flags().StringVar(&brand, "brand", "", "only this brand code")
	items.Flags().BoolVar(&noLink, "no-link", false, "do not rebuild family↔crop links at the end (30+ min; run `ingest link` once instead)")
	return items
}
