package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges/internal/vendors"
)

func vendorsCmd() *cobra.Command {
	c := &cobra.Command{Use: "vendors", Short: "Manufacturers' own per-finish product photographs"}
	var brand string
	var limit, workers int
	var dry bool
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch the maker's photo of each SKU in its finish and link it to the item (CAL, BAL, WMK, ETK)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 6)
			if err != nil {
				return err
			}
			defer pool.Close()
			f := &vendors.Fetcher{Pool: pool, DataDir: cfg.DataDir, Brand: brand, Limit: limit, DryRun: dry, Workers: workers}
			found, tried, err := f.Run(cmd.Context())
			fmt.Printf("%s: %d photos found of %d SKUs tried\n", brand, found, tried)
			return err
		},
	}
	fetch.Flags().StringVar(&brand, "brand", "", "Unilog brand code (required)")
	fetch.Flags().IntVar(&limit, "limit", 0, "stop after this many items")
	fetch.Flags().IntVar(&workers, "workers", 3, "parallel downloads")
	fetch.Flags().BoolVar(&dry, "dry", false, "look but do not store")
	fetch.MarkFlagRequired("brand")
	var cbrand string
	var climit int
	crawl := &cobra.Command{
		Use:   "crawl",
		Short: "Build the SKU → picture map for a maker whose picture names cannot be derived (ETK via the Wagtail API, CHI via the sitemap)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := (&vendors.Crawler{DataDir: cfg.DataDir, Brand: cbrand, Limit: climit, Pool: pool}).Run(cmd.Context())
			fmt.Printf("%s: %d pictures in the map\n", cbrand, n)
			return err
		},
	}
	crawl.Flags().StringVar(&cbrand, "brand", "", "Unilog brand code (required)")
	crawl.Flags().IntVar(&climit, "limit", 0, "stop after this many pages")
	crawl.MarkFlagRequired("brand")
	var ibrand, ipath string
	var idry bool
	var iminSide int
	imp := &cobra.Command{
		Use:   "import",
		Short: "Import a manufacturer image pack (zip or folder from a dealer portal, files named by SKU or model) as vendor masters",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			st, err := (&vendors.Pack{Pool: pool, DataDir: cfg.DataDir, Brand: ibrand, Path: ipath, DryRun: idry, MinSide: iminSide}).Run(cmd.Context())
			if st != nil {
				fmt.Printf("%s: files %d · pictures matched %d (%d items) · too small %d · unmatched %d · skipped %d\n", ibrand, st.Files, st.Matched, st.Items, st.Small, st.Unmatched, st.Skipped)
				for _, n := range st.UnmatchedNames {
					fmt.Println("  unmatched:", n)
				}
				if !idry && st.Matched > 0 {
					fmt.Println("next: georges assets build --source vendor && georges media pick && georges index build")
				}
			}
			return err
		},
	}
	imp.Flags().StringVar(&ibrand, "brand", "", "Unilog brand code (required)")
	imp.Flags().StringVar(&ipath, "path", "", "zip file or folder (required)")
	imp.Flags().BoolVar(&idry, "dry", false, "match and count, store nothing")
	imp.Flags().IntVar(&iminSide, "min-side", 400, "reject pictures smaller than this on their short side")
	imp.MarkFlagRequired("brand")
	imp.MarkFlagRequired("path")
	c.AddCommand(fetch, crawl, imp)
	return c
}
