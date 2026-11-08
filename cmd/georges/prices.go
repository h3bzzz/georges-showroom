package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"georges/internal/pricefile"
)

// pricesCmd: the company price file from Unilog's FTP → items.
func pricesCmd() *cobra.Command {
	c := &cobra.Command{Use: "prices", Short: "Company price file: fetch from FTP, inspect, load into items"}
	dir := func() string { return filepath.Join(cfg.DataDir, "pricefile") }
	client := func() (*pricefile.Client, error) { return pricefile.New(cfg.FTPHost, cfg.FTPUser, cfg.FTPPass) }

	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the files on the FTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			ents, err := cl.List(cmd.Context(), "")
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, e := range ents {
				kind := "file"
				if e.Dir {
					kind = "dir"
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", kind, e.Time.Format("2006-01-02 15:04"), e.Size, e.Name)
			}
			return tw.Flush()
		},
	})
	var remote string
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Download the newest price file (a-<date>.zip) into DATA_DIR/pricefile",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := client()
			if err != nil {
				return err
			}
			ents, err := cl.List(cmd.Context(), "")
			if err != nil {
				return err
			}
			var pick pricefile.Entry
			if remote != "" {
				for _, e := range ents {
					if e.Name == remote {
						pick = e
					}
				}
				if pick.Name == "" {
					return fmt.Errorf("%s is not on the server", remote)
				}
			} else {
				var ok bool
				if pick, ok = pricefile.Latest(ents); !ok {
					return fmt.Errorf("no price file on the server (%d entries)", len(ents))
				}
			}
			p, err := cl.Download(cmd.Context(), pick, dir())
			if err != nil {
				return err
			}
			fmt.Println(p)
			return nil
		},
	}
	fetch.Flags().StringVar(&remote, "name", "", "download this remote file instead of the newest")
	var n int
	inspect := &cobra.Command{
		Use:   "inspect [FILE]",
		Short: "Show the sheets, headers, detected columns and the first rows of a price file",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := localFile(dir(), args)
			if err != nil {
				return err
			}
			srcs, err := pricefile.Open(p)
			if err != nil {
				return err
			}
			for _, s := range srcs {
				fmt.Printf("== %s\n   headers: %s\n", s.Name, strings.Join(s.Headers, " | "))
				c := s.Cols
				fmt.Printf("   detected: brand=%q sku=%q upc=%q desc=%q finish=%q list=%q cost=%q price=%q lead=%q status=%q complete=%v\n",
					c.Brand, c.SKU, c.UPC, c.Description, c.Finish, c.List, c.Cost, c.Price, c.Lead, c.Status, c.Complete())
				i := 0
				brands := map[string]int{}
				total := 0
				_ = s.Rows(func(r pricefile.Row) bool {
					total++
					brands[r.Brand]++
					if i < n {
						fmt.Printf("   %-14s %-28s upc=%-14s list=%d cost=%d price=%d lead=%d disc=%v %s\n", r.Brand, r.SKU, r.UPC, r.ListCents, r.CostCents, r.PriceCents, r.LeadDays, r.Discontinued, r.Description)
						if i == 0 && len(r.Extra) > 0 {
							fmt.Printf("   extra: %v\n", r.Extra)
						}
					}
					i++
					return true
				})
				keys := make([]string, 0, len(brands))
				for k := range brands {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(a, b int) bool { return brands[keys[a]] > brands[keys[b]] })
				fmt.Printf("   %d rows, %d brands:", total, len(keys))
				for k, key := range keys {
					if k >= 25 {
						fmt.Printf(" …")
						break
					}
					fmt.Printf(" %s(%d)", key, brands[key])
				}
				fmt.Println()
			}
			return nil
		},
	}
	inspect.Flags().IntVar(&n, "rows", 5, "rows to print per sheet")
	var dry bool
	var brand string
	ingest := &cobra.Command{
		Use:   "ingest [FILE]",
		Short: "Load the newest downloaded price file into items (published price, cost, UPC, lead time)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := localFile(dir(), args)
			if err != nil {
				return err
			}
			srcs, err := pricefile.Open(p)
			if err != nil {
				return err
			}
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			st, err := (&pricefile.Ingester{Pool: pool, DryRun: dry, OnlyBrand: brand}).Run(cmd.Context(), srcs)
			if st != nil {
				fmt.Printf("rows %d · matched items %d · updated %d · unknown brand %d · unknown sku %d\n", st.Rows, st.Matched, st.Updated, st.UnknownBrand, st.UnknownSKU)
				keys := make([]string, 0, len(st.Missing))
				for k := range st.Missing {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(a, b int) bool { return st.Missing[keys[a]] > st.Missing[keys[b]] })
				for i, k := range keys {
					if i >= 20 {
						break
					}
					fmt.Printf("  unmatched %-30s %6d rows (matched %d)\n", k, st.Missing[k], st.Brands[k])
				}
			}
			if err != nil {
				return err
			}
			if !dry && st.Updated > 0 {
				fmt.Println("run `georges index build` to refresh prices in search")
			}
			return nil
		},
	}
	ingest.Flags().BoolVar(&dry, "dry", false, "parse and match, write nothing")
	ingest.Flags().StringVar(&brand, "brand", "", "only this brand (code, slug or name)")
	c.AddCommand(fetch, inspect, ingest)
	return c
}

// localFile picks the explicit path or the newest file in dir.
func localFile(dir string, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("no price file downloaded yet (run `georges prices fetch`): %w", err)
	}
	var best string
	var bestT int64
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().Unix() > bestT {
			best, bestT = filepath.Join(dir, e.Name()), fi.ModTime().Unix()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no price file in %s (run `georges prices fetch`)", dir)
	}
	return best, nil
}
