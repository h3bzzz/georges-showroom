package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges/internal/assets"
)

func assetsCmd() *cobra.Command {
	c := &cobra.Command{Use: "assets", Short: "Measure pictures and write their rendition ladder"}
	var limit, workers int
	var source string
	var shown bool
	build := &cobra.Command{
		Use:   "build",
		Short: "Process every asset whose ladder is missing or outdated (resumable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), int32(max(4, workers+2)))
			if err != nil {
				return err
			}
			defer pool.Close()
			b := &assets.Builder{Pool: pool, DataDir: cfg.DataDir, Workers: workers, Limit: limit, Source: source, Shown: shown}
			n, err := b.Run(cmd.Context())
			fmt.Printf("built %d assets\n", n)
			return err
		},
	}
	build.Flags().IntVar(&limit, "limit", 0, "stop after this many (0 = all)")
	build.Flags().IntVar(&workers, "workers", 0, "parallel workers (default: CPU count)")
	build.Flags().StringVar(&source, "source", "", "only this source: vendor | catalog | spec")
	build.Flags().BoolVar(&shown, "shown", false, "only assets that family_media uses")
	measure := &cobra.Command{
		Use:   "measure",
		Short: "Recompute measures (cut, fill, sharpness, LQIP) for assets missing them; no renditions",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), int32(max(4, workers+2)))
			if err != nil {
				return err
			}
			defer pool.Close()
			b := &assets.Builder{Pool: pool, DataDir: cfg.DataDir, Workers: workers, Limit: limit}
			n, err := b.Measure(cmd.Context())
			fmt.Printf("measured %d assets\n", n)
			return err
		},
	}
	measure.Flags().IntVar(&limit, "limit", 0, "stop after this many (0 = all)")
	measure.Flags().IntVar(&workers, "workers", 0, "parallel workers (default: CPU count)")
	c.AddCommand(build, measure)
	return c
}
