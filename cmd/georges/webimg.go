package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/store"
	"georges_/internal/webimg"
)

func webimgCmd() *cobra.Command {
	var brand string
	var limit int
	cmd := &cobra.Command{
		Use:   "webimages",
		Short: "For families without a catalog photo, look the SKU up on the manufacturer's own site and save its product image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			todo, err := st.FamiliesNeedingWebImage(cmd.Context(), brand, limit)
			if err != nil {
				return err
			}
			fmt.Printf("%d families to resolve\n", len(todo))
			cl := webimg.New(cfg.BraveAPIKey)
			if cfg.BraveAPIKey == "" {
				fmt.Println("note: BRAVE_API_KEY not set; only site-search heuristics will run (much lower hit rate)")
			}
			ok, fail := 0, 0
			start := time.Now()
			for _, t := range todo {
				res, err := cl.Resolve(cmd.Context(), t.Domain, t.SKU, t.FamilyKey)
				_ = st.MarkWebImageTried(cmd.Context(), t.FamilyID, err == nil)
				if err != nil {
					fail++
					continue
				}
				rel := filepath.Join("webimg", t.BrandSlug, safeFile(t.FamilyKey)+res.Ext)
				full := filepath.Join(cfg.DataDir, rel)
				_ = os.MkdirAll(filepath.Dir(full), 0o755)
				if err := os.WriteFile(full, res.Bytes, 0o644); err != nil {
					return err
				}
				if err := st.AddWebImage(cmd.Context(), t.FamilyID, res.ImageURL, filepath.ToSlash(rel), res.PageURL); err != nil {
					return err
				}
				ok++
				fmt.Printf("ok   %-22s %-16s %s\n", t.BrandSlug, t.FamilyKey, res.ImageURL)
			}
			fmt.Printf("web images: %d found, %d not found in %s\n", ok, fail, time.Since(start).Round(time.Second))
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand slug")
	cmd.Flags().IntVar(&limit, "limit", 200, "max families this run")
	return cmd
}
