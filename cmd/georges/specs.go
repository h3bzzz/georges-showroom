package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges/internal/specs"
)

func specsCmd() *cobra.Command {
	c := &cobra.Command{Use: "specs", Short: "Manufacturer specification sheets"}
	var brand string
	var limit int
	var dry bool
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch spec sheets by model number for one brand (CAL, CHI, NEW, KOL, KLL, LAV, BAL, BRZ)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			f := &specs.Fetcher{Pool: pool, DataDir: cfg.DataDir, Brand: brand, Limit: limit, DryRun: dry}
			found, tried, err := f.Run(cmd.Context())
			fmt.Printf("%s: %d sheets found of %d models tried\n", brand, found, tried)
			return err
		},
	}
	fetch.Flags().StringVar(&brand, "brand", "", "Unilog brand code (required)")
	fetch.Flags().IntVar(&limit, "limit", 0, "stop after this many families")
	fetch.Flags().BoolVar(&dry, "dry", false, "look but do not store")
	fetch.MarkFlagRequired("brand")
	var mbrand string
	var mdry bool
	mine := &cobra.Command{
		Use:   "mine",
		Short: "Read flow rate, spout height/reach, centers, bowl and cut-out sizes, mounting, material, certifications and feature bullets out of fetched sheets into families.facts",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := (&specs.Miner{Pool: pool, Brand: mbrand, DryRun: mdry}).Run(cmd.Context())
			fmt.Printf("%d families updated from spec sheets\n", n)
			return err
		},
	}
	mine.Flags().StringVar(&mbrand, "brand", "", "one brand code")
	mine.Flags().BoolVar(&mdry, "dry", false, "print the first changes, write nothing")
	c.AddCommand(fetch, mine)
	return c
}
