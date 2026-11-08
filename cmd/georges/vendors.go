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
	c.AddCommand(fetch, crawl)
	return c
}
