package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/site"
	"georges_/internal/store"
)

// crawlRunCmd works crawl_queue through the Chrome session: one product page per family.
// Each crawled page's finish dropdown lists the sibling SKU pages (SKU, finish, price, URL),
// which UpsertProduct stores and MarkCovered removes from the queue, so ~2.85M SKU URLs
// collapse to roughly one fetch per family. Resumable: state lives in Postgres.
func crawlRunCmd() *cobra.Command {
	var brand string
	var limit, batch, maxChallenge, maxAttempts int
	var delay time.Duration
	var images, login bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Work the crawl queue (sitemap load first): one page per family, siblings covered, resumable",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			brands, err := st.ListBrands(ctx)
			if err != nil {
				return err
			}
			slugs := make([]string, 0, len(brands))
			for _, b := range brands {
				slugs = append(slugs, b.Slug)
			}
			match := site.NewBrandMatcher(slugs)

			s, err := openSession(ctx, cfg, cfg.ChromeHeadless)
			if err != nil {
				return err
			}
			defer s.Close()
			p, err := s.Page()
			if err != nil {
				return err
			}
			if login {
				if err := s.Login(p); err != nil {
					fmt.Printf("warning: %v (continuing logged out; prices may be retail)\n", err)
				}
			}

			start := time.Now()
			done, covered, failed, challenges := 0, 0, 0, 0
			for {
				if limit > 0 && done >= limit {
					break
				}
				urls, err := st.NextCrawl(ctx, brand, maxAttempts, batch)
				if err != nil {
					return err
				}
				if len(urls) == 0 {
					fmt.Println("queue empty for this selection")
					break
				}
				for _, u := range urls {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if limit > 0 && done >= limit {
						break
					}
					prod, err := crawlOne(cmd, s, p, u, cfg.DataDir, st, images)
					switch {
					case err == nil:
						challenges = 0
						if err := st.MarkCrawled(ctx, prod.URL); err != nil {
							return err
						}
						if prod.URL != u {
							_ = st.MarkCrawled(ctx, u) // canonical differs from the sitemap spelling
						}
						slug, _, _ := match.Match(prod.URL)
						var sib []string
						for _, fo := range prod.FinishOptions {
							if fo.URL != "" && fo.URL != prod.URL && fo.URL != u {
								sib = append(sib, fo.URL)
							}
						}
						n, err := st.MarkCovered(ctx, sib, prod.URL, slug)
						if err != nil {
							return err
						}
						covered += int(n)
						done++
					case errors.Is(err, site.ErrNotProduct):
						failed++
						_ = st.MarkCrawlError(ctx, u, err.Error(), true)
						fmt.Printf("skip %s: %v\n", u, err)
					case errors.Is(err, errChallenge) || strings.Contains(err.Error(), "Cloudflare"):
						challenges++
						failed++
						_ = st.MarkCrawlError(ctx, u, err.Error(), false)
						if challenges >= maxChallenge {
							return fmt.Errorf("%d bot challenges in a row; run `georges browser login` (headed) and solve it, then restart", challenges)
						}
						wait := time.Duration(challenges) * 45 * time.Second
						fmt.Printf("challenge on %s; waiting %s\n", u, wait)
						select {
						case <-time.After(wait):
						case <-ctx.Done():
							return ctx.Err()
						}
					default:
						failed++
						_ = st.MarkCrawlError(ctx, u, err.Error(), false)
						fmt.Printf("FAIL %s: %v\n", u, err)
					}
					if (done+failed)%25 == 0 && done+failed > 0 {
						el := time.Since(start)
						fmt.Printf("  %d pages, %d siblings covered, %d failed, %.0f pages/min (%s)\n", done, covered, failed, float64(done)/el.Minutes(), el.Round(time.Second))
					}
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			fmt.Printf("done: %d pages, %d siblings covered, %d failed in %s\n", done, covered, failed, time.Since(start).Round(time.Second))
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand slug (as in the brands table, e.g. Native-trails)")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many successful pages (0 = until the queue is empty)")
	cmd.Flags().IntVar(&batch, "batch", 50, "URLs pulled from the queue per round")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", 3, "give up on a URL after this many failures")
	cmd.Flags().IntVar(&maxChallenge, "max-challenges", 3, "stop after this many consecutive bot challenges")
	cmd.Flags().DurationVar(&delay, "delay", 800*time.Millisecond, "pause between page loads")
	cmd.Flags().BoolVar(&images, "images", false, "also save the old site's product pictures (reference only, never shown)")
	cmd.Flags().BoolVar(&login, "login", true, "log in with SITE_USER/SITE_PASS first (admin sees trade pricing)")
	return cmd
}

func crawlStatsCmd() *cobra.Command {
	var top int
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Crawl queue totals and the brands with the biggest backlog",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.CrawlStats(cmd.Context())
			if err != nil {
				return err
			}
			for _, r := range rows {
				last := ""
				if r.Last != nil {
					last = r.Last.Format("2006-01-02 15:04")
				}
				fmt.Printf("%-9s %-8s %9d  %3d brands  %s\n", r.Kind, r.Status, r.N, r.Brands, last)
			}
			bs, err := st.CrawlBrandStats(cmd.Context(), top)
			if err != nil {
				return err
			}
			if len(bs) > 0 {
				fmt.Printf("\n%-32s %8s %8s %8s %6s\n", "brand", "pending", "done", "covered", "errors")
				for _, b := range bs {
					fmt.Printf("%-32s %8d %8d %8d %6d\n", b.Brand, b.Pending, b.Done, b.Covered, b.Errors)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&top, "top", 25, "brands to list")
	return cmd
}
