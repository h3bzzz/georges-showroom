package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/store"
)

// catalogsInfoCmd mines price books and catalogs for product information beyond SKU and
// price (internal/catinfo) and merges it into product_families and products.finish.
func catalogsInfoCmd() *cobra.Command {
	var brand string
	var dry bool
	var dump, catalog int
	var family int64
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Extract specs, collections, bullets and finishes from catalog pages into family_facts and product_families",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			stats, samples, err := st.ExtractCatalogInfo(cmd.Context(), cfg.DataDir, store.InfoOpts{Brand: strings.ToUpper(brand), Catalog: catalog, Family: family, Dry: dry, Dump: dump})
			if err != nil {
				return err
			}
			for _, s := range samples {
				fmt.Printf("\n== family %d %s · %s (catalog %d p.%d)\n", s.FamilyID, s.Brand, s.Title, s.Catalog, s.Page)
				if s.Result.Collection != "" {
					fmt.Printf("  collection: %s (%.1f)\n", s.Result.Collection, s.Result.CollConf)
				}
				for _, f := range s.Result.Facts {
					fmt.Printf("  %-18s %s\n", f.Key+":", f.Value)
				}
				if len(s.Result.Finishes) > 0 {
					fmt.Printf("  finishes: %s\n", strings.Join(s.Result.Finishes, ", "))
				}
				for _, b := range s.Result.Bullets {
					fmt.Printf("  • %s\n", b)
				}
				if s.Result.Description != "" {
					fmt.Printf("  desc: %s\n", s.Result.Description)
				}
			}
			mode := "written"
			if dry {
				mode = "dry run"
			}
			fmt.Printf("\n%s: %d families, %d with facts (%d facts), %d collections, %d with bullets, %d descriptions, %d product finishes set\n",
				mode, stats.Families, stats.WithFacts, stats.Facts, stats.Collections, stats.Bullets, stats.Descriptions, stats.FinishesSet)
			keys := make([]string, 0, len(stats.ByKey))
			for k := range stats.ByKey {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool { return stats.ByKey[keys[i]] > stats.ByKey[keys[j]] })
			for _, k := range keys {
				fmt.Printf("  %-20s %6d\n", k, stats.ByKey[k])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "brand code (default: all brands with catalog items)")
	cmd.Flags().BoolVar(&dry, "dry", false, "extract and report without writing")
	cmd.Flags().IntVar(&dump, "dump", 0, "print this many sample extractions")
	cmd.Flags().IntVar(&catalog, "catalog", 0, "restrict to one catalog id (debugging)")
	cmd.Flags().Int64Var(&family, "family", 0, "dump only this family id (debugging)")
	return cmd
}
