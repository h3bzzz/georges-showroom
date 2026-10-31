package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"georges_/internal/store"
	"georges_/internal/vendorimg"
)

// vendorimgCmd pulls manufacturer master images for every SKU of brands whose CDN URL is a
// function of the SKU (see internal/vendorimg.Vendors), records them as product_images
// source='web' and refreshes family_best so cards switch to the master immediately.
func vendorimgCmd() *cobra.Command {
	var brand string
	var limit int
	var rps float64
	var retry bool
	cmd := &cobra.Command{
		Use:   "vendorimages",
		Short: "Fetch 2000px white-background masters from brand CDNs keyed by SKU (Brizo today)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			cl := vendorimg.New(rps)
			dir := filepath.Join(cfg.DataDir, "webimg")
			start, got, miss, skipped, total := time.Now(), 0, 0, 0, 0
			for i := range vendorimg.Vendors {
				v := &vendorimg.Vendors[i]
				if brand != "" && brand != v.Code {
					continue
				}
				if retry {
					if _, err := st.Pool.Exec(ctx, `DELETE FROM product_images i USING products p JOIN product_families f ON f.id=p.family_id JOIN brands b ON b.id=f.brand_id
						WHERE i.product_id=p.id AND i.source='web' AND i.url LIKE 'miss:%' AND b.code=$1`, v.Code); err != nil {
						return err
					}
				}
				rows, err := st.Pool.Query(ctx, `
					SELECT p.id, p.family_id, p.sku FROM products p
					JOIN product_families f ON f.id=p.family_id JOIN brands b ON b.id=f.brand_id
					WHERE b.code=$1 AND NOT EXISTS (SELECT 1 FROM product_images i WHERE i.product_id=p.id AND i.source='web')
					ORDER BY f.tier, f.id, p.sku LIMIT $2`, v.Code, limit)
				if err != nil {
					return err
				}
				type row struct {
					ID, FamilyID int64
					SKU          string
				}
				todo, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
				if err != nil {
					return err
				}
				fmt.Printf("%s: %d SKUs to try on %s\n", v.Code, len(todo), v.Host)
				for _, r := range todo {
					total++
					res, err := cl.Fetch(ctx, v, r.SKU, dir)
					if errors.Is(err, vendorimg.ErrNoScheme) {
						skipped++
						continue
					}
					if err != nil {
						if ctx.Err() != nil {
							return ctx.Err()
						}
						fmt.Printf("FAIL %s: %v\n", r.SKU, err)
						continue
					}
					if res == nil {
						miss++
						if err := st.MarkProductImageMiss(ctx, r.ID); err != nil {
							return err
						}
						continue
					}
					rel, _ := filepath.Rel(cfg.DataDir, res.Path)
					if err := st.AddProductWebImage(ctx, r.FamilyID, r.ID, res.URL, filepath.ToSlash(rel)); err != nil {
						return err
					}
					got++
					if got%25 == 0 {
						fmt.Printf("  %d masters, %d misses of %d (%s)\n", got, miss, total, time.Since(start).Round(time.Second))
					}
				}
			}
			fmt.Printf("fetched %d masters, %d SKUs without one, %d skipped (no url scheme), in %s; refreshing family_best…\n", got, miss, skipped, time.Since(start).Round(time.Second))
			return st.RefreshFamilyBest(ctx)
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().IntVar(&limit, "limit", 100000, "max SKUs per vendor this run")
	cmd.Flags().BoolVar(&retry, "retry-misses", false, "forget earlier 'no image' answers for the vendor and ask again (after a resolver change)")
	cmd.Flags().Float64Var(&rps, "rps", 3, "requests per second per vendor")
	return cmd
}
