package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/config"
	"georges_/internal/logos"
	"georges_/internal/store"
)

func logosCmd() *cobra.Command {
	var list string
	var workers int
	var force, fromSite bool
	cmd := &cobra.Command{
		Use:   "logos",
		Short: "Fetch manufacturer logos from the manufacturers' own sites into DATA_DIR/logos and set brands.logo / featured",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			if fromSite {
				return logosFromSite(cmd, cfg, st)
			}
			f, err := os.Open(list)
			if err != nil {
				return err
			}
			defer f.Close()
			type row struct {
				slug, domain string
				featured     bool
			}
			var rows []row
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				p := strings.Split(sc.Text(), "\t")
				if len(p) < 2 || p[0] == "slug" || p[1] == "" {
					continue
				}
				rows = append(rows, row{p[0], p[1], len(p) > 2 && strings.TrimSpace(p[2]) == "1"})
			}
			dir := filepath.Join(cfg.DataDir, "logos")
			_ = os.MkdirAll(dir, 0o755)
			cl := logos.New()
			overrides := readOverrides("data/brand_logo_overrides.tsv")
			var mu sync.Mutex
			ok, fail := 0, 0
			jobs := make(chan row)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for r := range jobs {
						_ = st.SetBrandFeatured(cmd.Context(), r.slug, r.featured, r.domain)
						if u, found := overrides[r.slug]; found {
							// hand-picked asset (official press logo, Wikimedia vector…): always wins
							res, err := cl.FetchURL(cmd.Context(), u)
							if err != nil {
								fmt.Printf("FAIL override %s: %v\n", r.slug, err)
							} else {
								name := r.slug + res.Ext
								for _, old := range mustGlob(filepath.Join(dir, r.slug+".*")) {
									if filepath.Base(old) != name {
										_ = os.Remove(old)
									}
								}
								if err := os.WriteFile(filepath.Join(dir, name), res.Bytes, 0o644); err == nil {
									_ = st.SetBrandLogoBySlug(cmd.Context(), r.slug, "/files/logos/"+name)
									mu.Lock()
									ok++
									mu.Unlock()
									fmt.Printf("ok   %-28s override         %s\n", r.slug, u)
									continue
								}
							}
						}
						existing, _ := filepath.Glob(filepath.Join(dir, r.slug+".*"))
						if len(existing) > 0 && !force {
							if strings.HasSuffix(existing[0], ".svg") {
								if b, err := os.ReadFile(existing[0]); err == nil {
									_ = os.WriteFile(existing[0], logos.DarkenSVG(b), 0o644)
								}
							}
							if good, why := logos.Audit(existing[0]); good {
								rel := filepath.ToSlash(filepath.Join("logos", filepath.Base(existing[0])))
								_ = st.SetBrandLogoBySlug(cmd.Context(), r.slug, "/files/"+rel)
								mu.Lock()
								ok++
								mu.Unlock()
								continue
							} else {
								fmt.Printf("bad  %-28s %s (%s) → refetch\n", r.slug, filepath.Base(existing[0]), why)
								for _, old := range existing {
									_ = os.Remove(old)
								}
							}
						}
						res, err := cl.Fetch(cmd.Context(), r.domain)
						if err == nil {
							if good, why := logos.Audit(writeTemp(res)); !good {
								fmt.Printf("bad  %-28s site logo rejected (%s)\n", r.slug, why)
								err = fmt.Errorf("rejected: %s", why)
							}
						}
						if err != nil {
							if wres, werr := cl.Wikipedia(cmd.Context(), brandName(r.slug)); werr == nil {
								if good, _ := logos.Audit(writeTemp(wres)); good {
									res, err = wres, nil
								}
							}
						}
						if err != nil {
							mu.Lock()
							fail++
							mu.Unlock()
							_ = st.SetBrandLogoBySlug(cmd.Context(), r.slug, "")
							fmt.Printf("FAIL %-28s %s: %v (text wordmark will be shown)\n", r.slug, r.domain, err)
							continue
						}
						name := r.slug + res.Ext
						if err := os.WriteFile(filepath.Join(dir, name), res.Bytes, 0o644); err != nil {
							fmt.Printf("FAIL write %s: %v\n", name, err)
							continue
						}
						_ = st.SetBrandLogoBySlug(cmd.Context(), r.slug, "/files/logos/"+name)
						mu.Lock()
						ok++
						mu.Unlock()
						fmt.Printf("ok   %-28s %-16s %s\n", r.slug, res.How, res.URL)
					}
				}()
			}
			for _, r := range rows {
				jobs <- r
			}
			close(jobs)
			wg.Wait()
			fmt.Printf("logos: %d ok, %d failed\n", ok, fail)
			return nil
		},
	}
	cmd.Flags().StringVar(&list, "list", "data/brand_domains.tsv", "slug<TAB>domain<TAB>featured")
	cmd.Flags().IntVar(&workers, "workers", 6, "parallel fetches")
	cmd.Flags().BoolVar(&force, "force", false, "refetch even if a file exists")
	cmd.Flags().BoolVar(&fromSite, "from-site", false, "pull the dealer brand marks through the Chrome session (run `browser login` first)")
	return cmd
}

// logosFromSite fetches each brand's dealer mark (/<CODE>-<Brand-Name>.Gif) through the
// authenticated Chrome session, audits it, and stores it as the brand logo.
func logosFromSite(cmd *cobra.Command, cfg *config.Config, st *store.Store) error {
	brands, err := st.BrandCards(cmd.Context(), false)
	if err != nil {
		return err
	}
	s, err := openSession(cmd.Context(), cfg, cfg.ChromeHeadless)
	if err != nil {
		return err
	}
	defer s.Close()
	p, err := s.Page()
	if err != nil {
		return err
	}
	dir := filepath.Join(cfg.DataDir, "logos")
	_ = os.MkdirAll(dir, 0o755)
	ok, fail := 0, 0
	for _, b := range brands {
		if b.Code == "" {
			continue
		}
		parts := strings.Split(b.Slug, "-")
		for i, w := range parts {
			if w != "" {
				parts[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		u := cfg.SiteBaseURL + "/" + b.Code + "-" + strings.Join(parts, "-") + ".Gif"
		data, err := s.FetchImage(p, u)
		if err != nil {
			fail++
			fmt.Printf("FAIL %-28s %s: %v\n", b.Slug, u, err)
			continue
		}
		path := filepath.Join(dir, b.Slug+".gif")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		if good, why := logos.Audit(path); !good {
			fmt.Printf("bad  %-28s %s\n", b.Slug, why)
			_ = os.Remove(path)
			fail++
			continue
		}
		for _, old := range mustGlob(filepath.Join(dir, b.Slug+".*")) {
			if filepath.Base(old) != b.Slug+".gif" {
				_ = os.Remove(old)
			}
		}
		_ = st.SetBrandLogoBySlug(cmd.Context(), b.Slug, "/files/logos/"+b.Slug+".gif")
		ok++
		fmt.Printf("ok   %-28s %s\n", b.Slug, u)
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Printf("dealer marks: %d ok, %d failed\n", ok, fail)
	return nil
}

// readOverrides loads slug<TAB>url pairs; missing file is fine.
func readOverrides(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(strings.TrimSpace(sc.Text()), "\t")
		if len(p) >= 2 && !strings.HasPrefix(p[0], "#") && p[0] != "slug" {
			out[p[0]] = p[1]
		}
	}
	return out
}

func mustGlob(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

// writeTemp stores a fetched logo in a temp file so Audit can inspect it.
func writeTemp(res *logos.Result) string {
	f, err := os.CreateTemp("", "logo-*"+res.Ext)
	if err != nil {
		return ""
	}
	_, _ = f.Write(res.Bytes)
	f.Close()
	return f.Name()
}

// brandName turns a slug like "Rocky-mountain-hardware" into "Rocky mountain hardware".
func brandName(slug string) string {
	return strings.ReplaceAll(slug, "-", " ")
}
