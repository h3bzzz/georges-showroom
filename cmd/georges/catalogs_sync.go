package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/catalogs"
	"georges_/internal/store"
)

// readCatalogList reads data/catalogs.tsv ("CODE/name<TAB>pages").
func readCatalogList(path string) ([]catalogs.Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []catalogs.Book
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "book\t") {
			continue
		}
		field := strings.Split(line, "\t")[0]
		b, err := catalogs.ParseBook(field)
		if err != nil {
			continue
		}
		out = append(out, b)
	}
	return out, sc.Err()
}

func catalogsSyncCmd() *cobra.Command {
	var list string
	sync := &cobra.Command{
		Use:   "sync",
		Short: "Probe every catalog in data/catalogs.tsv and store metadata + text layers in Postgres",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			books, err := readCatalogList(list)
			if err != nil {
				return err
			}
			cl := catalogs.NewClient(8)
			ok, fail, textPages := 0, 0, 0
			thumbDir := filepath.Join(cfg.DataDir, "catalogs")
			for _, b := range books {
				info, err := cl.Probe(cmd.Context(), b)
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", b, err)
					fail++
					continue
				}
				if _, err := cl.FetchThumb(cmd.Context(), b, thumbDir); err != nil {
					fmt.Printf("     (no thumbnail for %s: %v)\n", b, err)
				}
				id, err := st.UpsertCatalog(cmd.Context(), b.Code, b.Name, info.Title, info.Pages, info.CreatedAt, info.HasText, false)
				if err != nil {
					return err
				}
				for i, t := range info.PageTexts {
					if strings.TrimSpace(t) == "" {
						continue
					}
					if err := st.UpsertCatalogPage(cmd.Context(), id, i+1, "", t); err != nil {
						return err
					}
					textPages++
				}
				ok++
				fmt.Printf("ok   %-55s %4d pages text=%v\n", b, info.Pages, info.HasText)
			}
			fmt.Printf("synced %d catalogs (%d failed), %d text pages stored\n", ok, fail, textPages)
			return nil
		},
	}
	sync.Flags().StringVar(&list, "list", "data/catalogs.tsv", "catalog list file")

	var workers int
	var rps float64
	var only string
	fetchAll := &cobra.Command{
		Use:   "fetch-all",
		Short: "Download page images for every catalog in Postgres (resumable; skips existing files)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.ListCatalogs(cmd.Context(), only)
			if err != nil {
				return err
			}
			cl := catalogs.NewClient(rps)
			dir := filepath.Join(cfg.DataDir, "catalogs")
			start := time.Now()
			var totalBytes int64
			for _, c := range rows {
				if c.Pages == 0 {
					continue
				}
				b := catalogs.Book{Code: c.BrandCode, Name: c.Name}
				var got, skipped int
				err := cl.FetchPages(cmd.Context(), b, 1, c.Pages, dir, workers, func(p, sz int, sk bool) {
					if sk {
						skipped++
					} else {
						got++
						totalBytes += int64(sz)
					}
					if !sk {
						_ = st.UpsertCatalogPage(cmd.Context(), c.ID, p, filepath.Join(dir, b.Code, b.Name, strconv.Itoa(p)), "")
					}
				})
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", b, err)
					continue
				}
				_, _ = st.UpsertCatalog(cmd.Context(), c.BrandCode, c.Name, c.Title, c.Pages, "", c.HasText, true)
				fmt.Printf("ok   %-55s %4d pages (%d new, %d cached) %.1f GB total, %s\n", b, c.Pages, got, skipped, float64(totalBytes)/1e9, time.Since(start).Round(time.Second))
			}
			return nil
		},
	}
	fetchAll.Flags().IntVar(&workers, "workers", 4, "parallel downloads")
	fetchAll.Flags().Float64Var(&rps, "rps", 8, "requests per second cap")
	fetchAll.Flags().StringVar(&only, "brand", "", "only this brand code")

	cmd := &cobra.Command{Use: "catalogs-db", Hidden: true}
	cmd.AddCommand(sync, fetchAll)
	return cmd
}
