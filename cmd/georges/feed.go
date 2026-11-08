package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"georges/internal/feed"
)

// feedCmd: the Unilog XML data feed → feed_products → families/items.
func feedCmd() *cobra.Command {
	c := &cobra.Command{Use: "feed", Short: "Unilog data feed: load the brand XML files, derive families and items"}
	var brand string
	load := &cobra.Command{
		Use:   "load",
		Short: "Upsert every mirrored brand XML (DATA_DIR/feed/xml) into feed_products",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := (&feed.Loader{Pool: pool, Dir: filepath.Join(cfg.DataDir, "feed", "xml"), Brand: brand}).Run(cmd.Context())
			fmt.Printf("%d feed products loaded\n", n)
			return err
		},
	}
	load.Flags().StringVar(&brand, "brand", "", "one brand code")
	var dry bool
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Create families and items for feed products the site does not have yet; refresh copy, bullets, facts and finishes on the ones it has",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			st, err := (&feed.Applier{Pool: pool, Brand: brand, DryRun: dry}).Run(cmd.Context())
			if st != nil {
				fmt.Printf("feed rows %d · existing items refreshed %d · new items %d · new families %d · families enriched %d\n", st.Rows, st.Matched, st.NewItems, st.NewFamilies, st.Enriched)
			}
			return err
		},
	}
	apply.Flags().StringVar(&brand, "brand", "", "one brand code")
	apply.Flags().BoolVar(&dry, "dry", false, "count only, write nothing")
	c.AddCommand(load, apply)
	return c
}
