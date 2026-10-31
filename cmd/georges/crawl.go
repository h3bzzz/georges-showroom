package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/browser"
	"georges_/internal/site"
	"georges_/internal/store"
)

// productJSONPath mirrors the site path: DATA_DIR/products/<Brand>/<slug>.json
func productJSONPath(dataDir, url string) string {
	base := filepath.Base(url)
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".HTM"), ".htm")
	brand := base
	if i := strings.Index(base, "-"); i > 0 {
		brand = base[:i]
	}
	return filepath.Join(dataDir, "products", brand, base+".json")
}

func crawlCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "crawl", Short: "Fetch site pages through the Chrome session and save parsed JSON"}
	var delay time.Duration
	var toDB bool

	product := &cobra.Command{
		Use:   "product URL...",
		Short: "Fetch product pages → DATA_DIR/products/<brand>/<page>.json (and Postgres with --db)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			s, err := openSession(cmd.Context(), cfg, cfg.ChromeHeadless)
			if err != nil {
				return err
			}
			defer s.Close()
			p, err := s.Page()
			if err != nil {
				return err
			}
			var st *store.Store
			if toDB {
				if st, err = store.Open(cmd.Context(), cfg.DatabaseURL); err != nil {
					return err
				}
				defer st.Close()
			}
			for i, u := range args {
				if i > 0 {
					time.Sleep(delay)
				}
				if _, err := crawlOne(cmd, s, p, u, cfg.DataDir, st, true); err != nil {
					fmt.Printf("FAIL %s: %v\n", u, err)
				}
			}
			return nil
		},
	}
	product.Flags().DurationVar(&delay, "delay", 800*time.Millisecond, "pause between page loads")
	product.Flags().BoolVar(&toDB, "db", false, "also upsert into Postgres")

	var pages int
	var follow bool
	listing := &cobra.Command{
		Use:   "listing URL",
		Short: "Walk a category/brand/search listing (24 families per page), save listing JSON, optionally crawl each product",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			s, err := openSession(cmd.Context(), cfg, cfg.ChromeHeadless)
			if err != nil {
				return err
			}
			defer s.Close()
			p, err := s.Page()
			if err != nil {
				return err
			}
			var st *store.Store
			if toDB {
				if st, err = store.Open(cmd.Context(), cfg.DatabaseURL); err != nil {
					return err
				}
				defer st.Close()
			}
			u := args[0]
			for n := 1; pages == 0 || n <= pages; n++ {
				if err := s.Navigate(p, u, 60*time.Second); err != nil {
					return err
				}
				html, err := browser.HTML(p)
				if err != nil {
					return err
				}
				l, err := site.ParseListing(strings.NewReader(html), u)
				if err != nil {
					return fmt.Errorf("%s: %w", u, err)
				}
				out := filepath.Join(cfg.DataDir, "listings", safeFile(u)+".json")
				_ = os.MkdirAll(filepath.Dir(out), 0o755)
				b, _ := json.MarshalIndent(l, "", "  ")
				_ = os.WriteFile(out, b, 0o644)
				fmt.Printf("page %d/%d: %d items (%s)\n", l.Page, l.TotalPages, len(l.Items), l.Title)
				if follow {
					for _, it := range l.Items {
						if _, err := os.Stat(productJSONPath(cfg.DataDir, it.URL)); err == nil {
							continue
						}
						time.Sleep(delay)
						if _, err := crawlOne(cmd, s, p, it.URL, cfg.DataDir, st, true); err != nil {
							fmt.Printf("  FAIL %s: %v\n", it.URL, err)
						}
					}
				}
				if l.NextURL == "" || l.Page >= l.TotalPages {
					break
				}
				u = l.NextURL
				time.Sleep(delay)
			}
			return nil
		},
	}
	listing.Flags().IntVar(&pages, "pages", 0, "max listing pages to walk (0 = all)")
	listing.Flags().BoolVar(&follow, "follow", false, "also crawl every product on each page")
	listing.Flags().DurationVar(&delay, "delay", 800*time.Millisecond, "pause between page loads")
	listing.Flags().BoolVar(&toDB, "db", false, "also upsert into Postgres")

	var limit int
	images := &cobra.Command{
		Use:   "images",
		Short: "Download site images that have no local copy yet (product images, then brand logos)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			s, err := openSession(cmd.Context(), cfg, cfg.ChromeHeadless)
			if err != nil {
				return err
			}
			defer s.Close()
			p, err := s.Page()
			if err != nil {
				return err
			}
			logos, _ := st.PendingLogos(cmd.Context())
			for _, b := range logos {
				rel := filepath.Join("images", "logos", filepath.Base(b.Logo))
				data, err := s.FetchImage(p, b.Logo)
				if err != nil {
					fmt.Printf("FAIL logo %s: %v\n", b.Name, err)
					continue
				}
				_ = os.MkdirAll(filepath.Join(cfg.DataDir, "images", "logos"), 0o755)
				if err := os.WriteFile(filepath.Join(cfg.DataDir, rel), data, 0o644); err != nil {
					return err
				}
				_ = st.SetBrandLogo(cmd.Context(), b.ID, "/files/"+filepath.ToSlash(rel))
				fmt.Printf("logo %s\n", b.Name)
				time.Sleep(delay)
			}
			urls, err := st.PendingImages(cmd.Context(), limit)
			if err != nil {
				return err
			}
			for _, u := range urls {
				rel := imageRelPath(u)
				full := filepath.Join(cfg.DataDir, rel)
				if _, err := os.Stat(full); err != nil {
					data, err := s.FetchImage(p, u)
					if err != nil {
						fmt.Printf("FAIL %s: %v\n", u, err)
						continue
					}
					_ = os.MkdirAll(filepath.Dir(full), 0o755)
					if err := os.WriteFile(full, data, 0o644); err != nil {
						return err
					}
					time.Sleep(delay)
				}
				_ = st.SetImagePath(cmd.Context(), u, filepath.ToSlash(rel), 0, 0)
				fmt.Printf("img  %s\n", rel)
			}
			return nil
		},
	}
	images.Flags().IntVar(&limit, "limit", 500, "max product images per run")
	images.Flags().DurationVar(&delay, "delay", 300*time.Millisecond, "pause between fetches")

	cmd.AddCommand(product, listing, images, crawlRunCmd(), crawlStatsCmd())
	return cmd
}

// crawlOne fetches one product page, saves its JSON (and, with images, the old site's product
// pictures for reference), and upserts it when st is set.
func crawlOne(cmd *cobra.Command, s *browser.Session, p browserPage, u, dataDir string, st *store.Store, images bool) (*site.Product, error) {
	if err := s.Navigate(p, u, 60*time.Second); err != nil {
		return nil, err
	}
	html, err := browser.HTML(p)
	if err != nil {
		return nil, err
	}
	if site.IsChallenge(html) {
		return nil, errChallenge
	}
	prod, err := site.ParseProduct(strings.NewReader(html), u)
	if err != nil {
		return nil, err
	}
	prod.LocalImages = map[string]string{}
	for _, img := range prod.Images {
		if !images {
			break
		}
		rel := imageRelPath(img)
		full := filepath.Join(dataDir, rel)
		if _, err := os.Stat(full); err == nil {
			prod.LocalImages[img] = rel
			continue
		}
		b, err := p.GetResource(img) // already loaded by the product page
		if err != nil || len(b) < 100 {
			if b, err = s.FetchImage(p, img); err != nil {
				continue
			}
		}
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if os.WriteFile(full, b, 0o644) == nil {
			prod.LocalImages[img] = rel
		}
	}
	out := productJSONPath(dataDir, prod.URL)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(prod, "", "  ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return nil, err
	}
	if st != nil {
		if _, err := st.UpsertProduct(cmd.Context(), prod); err != nil {
			return nil, err
		}
	}
	fmt.Printf("ok   %s %s $%.2f (%d finishes, %d imgs, %d docs)\n", prod.Brand, prod.SKU, float64(prod.PriceCents)/100, prod.FinishCount, len(prod.Images), len(prod.Documents))
	return prod, nil
}

var errChallenge = errors.New("bot challenge page (Cloudflare / iAmHuman)")

// imageRelPath maps https://www.georgesshowroom.com/NAT-FOO.JPEG → images/NAT/NAT-FOO.JPEG
func imageRelPath(u string) string {
	base := filepath.Base(strings.SplitN(u, "?", 2)[0])
	brand := "misc"
	if i := strings.Index(base, "-"); i > 0 && i <= 4 {
		brand = strings.ToUpper(base[:i])
	}
	return filepath.Join("images", brand, base)
}

func safeFile(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, u)
}
