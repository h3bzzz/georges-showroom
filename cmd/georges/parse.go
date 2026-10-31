package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"georges_/internal/site"
)

func parseCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "parse", Short: "Parse saved HTML files offline (for testing the parsers)"}
	product := &cobra.Command{
		Use:   "product FILE...",
		Short: "Parse product HTML → DATA_DIR/products/<brand>/<page>.json",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			for _, f := range args {
				fh, err := os.Open(f)
				if err != nil {
					return err
				}
				p, err := site.ParseProduct(fh, "")
				fh.Close()
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", f, err)
					continue
				}
				out := productJSONPath(cfg.DataDir, p.URL)
				_ = os.MkdirAll(filepath.Dir(out), 0o755)
				b, _ := json.MarshalIndent(p, "", "  ")
				if err := os.WriteFile(out, b, 0o644); err != nil {
					return err
				}
				fmt.Printf("ok   %s %s → %s\n", p.Brand, p.SKU, out)
			}
			return nil
		},
	}
	listing := &cobra.Command{
		Use:   "listing FILE",
		Short: "Parse a listing HTML and print JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fh, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer fh.Close()
			l, err := site.ParseListing(fh, "")
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(l, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.AddCommand(product, listing)
	return cmd
}
