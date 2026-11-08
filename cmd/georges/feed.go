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
	var limit int
	images := &cobra.Command{
		Use:   "images",
		Short: "Fetch the feed's product shots (ftp images/<BRAND>/) for families with no picture, as lowest-priority assets",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			im := &feed.Images{Pool: pool, DataDir: cfg.DataDir, Host: cfg.FTPHost, User: cfg.FTPUser, Pass: cfg.FTPPass, Brand: brand, Limit: limit, DryRun: dry}
			n, err := im.Run(cmd.Context())
			fmt.Printf("%d feed pictures stored; run `assets build --source feed`, `media pick`, `index build`\n", n)
			return err
		},
	}
	images.Flags().StringVar(&brand, "brand", "", "one brand code")
	images.Flags().IntVar(&limit, "limit", 0, "stop after this many families")
	images.Flags().BoolVar(&dry, "dry", false, "count only")
	disc := &cobra.Command{
		Use:   "discontinued CSV",
		Short: "Flag items listed in Unilog's discontinuedProducts sheet (CSV export)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, flagged, err := feed.MarkDiscontinued(cmd.Context(), pool, args[0])
			fmt.Printf("%d rows read, %d items flagged discontinued\n", n, flagged)
			return err
		},
	}
	c.AddCommand(load, apply, images, disc)
	return c
}
