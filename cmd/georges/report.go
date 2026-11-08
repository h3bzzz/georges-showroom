package main

import (
	"os"

	"github.com/spf13/cobra"

	"georges/internal/report"
)

func reportCmd() *cobra.Command {
	var brand string
	var limit int
	c := &cobra.Command{Use: "report", Short: "Catalog scoreboards"}
	cov := &cobra.Command{
		Use:   "coverage",
		Short: "Per brand: designs, pictured, maker photo, priced, finishes, readable title",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			rows, total, err := report.Coverage(cmd.Context(), pool, brand)
			if err != nil {
				return err
			}
			report.Print(os.Stdout, rows, total, limit)
			return nil
		},
	}
	cov.Flags().StringVar(&brand, "brand", "", "one brand code")
	cov.Flags().IntVar(&limit, "limit", 25, "rows to print (0 = all)")
	c.AddCommand(cov)
	return c
}
