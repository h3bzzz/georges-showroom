// Command georges is the operator CLI for the George's rebuild: data pipelines, browser
// session management, and (later) the web server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/browser"
	"georges_/internal/catalogs"
	"georges_/internal/config"
	"georges_/internal/pricefile"
	"georges_/internal/site"
	"georges_/internal/sitemap"
	"georges_/internal/specs"
	"georges_/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := &cobra.Command{
		Use:   "georges",
		Short: "George's Showroom data + site tooling (no arguments = start the site and open it)",
		RunE:  runSite,
	}
	root.AddCommand(catalogsCmd(), specsCmd(), pricefileCmd(), browserCmd(), sitemapCmd(), dbCmd(), ingestCmd(), crawlCmd(), serveCmd(), parseCmd(), waybackCmd(), logosCmd(), webimgCmd(), vendorimgCmd(), upscaleCmd())
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func mustConfig() *config.Config {
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return c
}

// ---------- catalogs ----------

func catalogsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catalogs", Short: "Vendor catalog flipbooks (catalogs.myplumbingshowroom.com)"}
	probe := &cobra.Command{
		Use:   "probe CODE/name",
		Short: "Show page count, title and whether a text layer exists",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := catalogs.ParseBook(args[0])
			if err != nil {
				return err
			}
			info, err := catalogs.NewClient(4).Probe(cmd.Context(), b)
			if err != nil {
				return err
			}
			fmt.Printf("book:     %s\ntitle:    %s\npages:    %d\ncreated:  %s\nhas_text: %v\n", info.Book, info.Title, info.Pages, info.CreatedAt, info.HasText)
			if info.HasText {
				n := 0
				for _, t := range info.PageTexts {
					if strings.TrimSpace(t) != "" {
						n++
					}
				}
				fmt.Printf("text pages: %d\n", n)
			}
			return nil
		},
	}
	var pages string
	var workers int
	var rps float64
	fetch := &cobra.Command{
		Use:   "fetch CODE/name",
		Short: "Download page images (and text layer) into DATA_DIR/catalogs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			b, err := catalogs.ParseBook(args[0])
			if err != nil {
				return err
			}
			cl := catalogs.NewClient(rps)
			info, err := cl.Probe(cmd.Context(), b)
			if err != nil {
				return err
			}
			from, to := 1, info.Pages
			if pages != "" {
				if from, to, err = parseRange(pages, info.Pages); err != nil {
					return err
				}
			}
			dir := filepath.Join(cfg.DataDir, "catalogs")
			n, err := catalogs.WriteText(info, dir)
			if err != nil {
				return err
			}
			fmt.Printf("%s: %d pages, text layer: %v (%d pages written)\n", b, info.Pages, info.HasText, n)
			var got, skipped, bytes int
			start := time.Now()
			err = cl.FetchPages(cmd.Context(), b, from, to, dir, workers, func(p, sz int, sk bool) {
				if sk {
					skipped++
				} else {
					got++
					bytes += sz
				}
				if (got+skipped)%25 == 0 {
					fmt.Printf("  %d/%d pages (%.1f MB, %s)\n", got+skipped, to-from+1, float64(bytes)/1e6, time.Since(start).Round(time.Second))
				}
			})
			fmt.Printf("done: %d downloaded, %d skipped, %.1f MB in %s → %s\n", got, skipped, float64(bytes)/1e6, time.Since(start).Round(time.Second), filepath.Join(dir, b.Code))
			return err
		},
	}
	fetch.Flags().StringVar(&pages, "pages", "", "page range, e.g. 1-20 (default: all)")
	fetch.Flags().IntVar(&workers, "workers", 4, "parallel downloads")
	fetch.Flags().Float64Var(&rps, "rps", 6, "requests per second cap")
	cmd.AddCommand(probe, fetch)
	for _, sub := range catalogsSyncCmd().Commands() {
		cmd.AddCommand(sub)
	}
	cmd.AddCommand(catalogsExtractCmds()...)
	cmd.AddCommand(catalogsPipelineCmd(), catalogsItemsCmd(), catalogsTocCmd(), tocDebugCmd(), catalogsAuditCmd(), catalogsTidyCmd(), catalogsRejectCmd(), catalogsSheetCmd(), catalogsVisionCmd(), catalogsDedupeCmd(), catalogsInfoCmd())
	return cmd
}

func parseRange(s string, max int) (int, int, error) {
	a, b, ok := strings.Cut(s, "-")
	from, err := strconv.Atoi(strings.TrimSpace(a))
	if err != nil {
		return 0, 0, fmt.Errorf("bad range %q", s)
	}
	to := from
	if ok {
		if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil {
			return 0, 0, fmt.Errorf("bad range %q", s)
		}
	}
	if from < 1 || to < from || to > max {
		return 0, 0, fmt.Errorf("range %q outside 1-%d", s, max)
	}
	return from, to, nil
}

// ---------- specs ----------

func specsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "specs", Short: "Spec/install sheets on documents.cx1accel.com (no Cloudflare)"}
	var text bool
	fetch := &cobra.Command{
		Use:   "fetch URL...",
		Short: "Download PDFs into DATA_DIR/docs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			cl := specs.NewClient(4)
			for _, u := range args {
				r, err := cl.Fetch(cmd.Context(), u, filepath.Join(cfg.DataDir, "docs"), text)
				if err != nil {
					fmt.Printf("FAIL %s: %v\n", u, err)
					continue
				}
				fmt.Printf("ok   %s (%d bytes, %d pages) → %s\n", u, r.Bytes, r.Pages, r.Path)
				if text {
					fmt.Println(strings.TrimSpace(r.Text))
				}
			}
			return nil
		},
	}
	fetch.Flags().BoolVar(&text, "text", false, "also run pdftotext and print the text")
	cmd.AddCommand(fetch, specsSyncCmd())
	return cmd
}

// ---------- pricefile ----------

func pricefileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pricefile", Short: "Daily company price file over FTP"}
	ls := &cobra.Command{
		Use:   "ls [dir]",
		Short: "List files on the FTP server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			cl, err := pricefile.New(cfg.FTPHost, cfg.FTPUser, cfg.FTPPass)
			if err != nil {
				return err
			}
			dir := ""
			if len(args) == 1 {
				dir = args[0]
			}
			ents, err := cl.List(cmd.Context(), dir)
			if err != nil {
				return err
			}
			for _, e := range ents {
				kind := "f"
				if e.Dir {
					kind = "d"
				}
				fmt.Printf("%s %12d  %s  %s\n", kind, e.Size, e.Time.Format("2006-01-02 15:04"), e.Name)
			}
			return nil
		},
	}
	get := &cobra.Command{
		Use:   "get FILE...",
		Short: "Download files into DATA_DIR/pricefile",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			cl, err := pricefile.New(cfg.FTPHost, cfg.FTPUser, cfg.FTPPass)
			if err != nil {
				return err
			}
			for _, f := range args {
				p, n, err := cl.Download(cmd.Context(), f, filepath.Join(cfg.DataDir, "pricefile"))
				if err != nil {
					return err
				}
				fmt.Printf("ok %s (%d bytes) → %s\n", f, n, p)
			}
			return nil
		},
	}
	cmd.AddCommand(ls, get)
	return cmd
}

// ---------- browser ----------

func openSession(ctx context.Context, cfg *config.Config, headless bool) (*browser.Session, error) {
	return browser.Open(ctx, browser.Options{
		ControlURL: cfg.ChromeControlURL,
		ChromeBin:  cfg.ChromeBin, ProfileDir: cfg.ChromeProfileDir, Headless: headless,
		BaseURL: cfg.SiteBaseURL, User: cfg.SiteUser, Pass: cfg.SitePass,
	})
}

func browserCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "browser", Short: "Chrome session used to get through Cloudflare"}
	login := &cobra.Command{
		Use:   "login",
		Short: "Open Chrome (headed), pass the Cloudflare challenge and log in; profile is persisted",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			s, err := openSession(cmd.Context(), cfg, false)
			if err != nil {
				return err
			}
			defer s.Close()
			p, err := s.Page()
			if err != nil {
				return err
			}
			if err := s.Login(p); err != nil {
				return err
			}
			fmt.Println("logged in; profile saved to", cfg.ChromeProfileDir)
			return nil
		},
	}
	var out string
	get := &cobra.Command{
		Use:   "get URL",
		Short: "Fetch one page through the session and print (or save) its HTML",
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
			if err := s.Navigate(p, args[0], 60*time.Second); err != nil {
				return err
			}
			html, err := browser.HTML(p)
			if err != nil {
				return err
			}
			if out == "" {
				fmt.Println(html)
				return nil
			}
			return os.WriteFile(out, []byte(html), 0o644)
		},
	}
	get.Flags().StringVarP(&out, "out", "o", "", "write HTML to this file")
	cmd.AddCommand(login, get)
	return cmd
}

// ---------- sitemap ----------

func sitemapCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sitemap", Short: "Site index (zgeorges-SiteIndex*.xml): fetch through Chrome, load into the crawl queue"}
	var limit int
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Pull every sitemap through the browser session into DATA_DIR/sitemap/*.txt and queue the URLs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			loader, err := newQueueLoader(cmd.Context(), st)
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
			idx := cfg.SiteBaseURL + "/zgeorges-SiteIndex.xml"
			if err := s.Navigate(p, idx, 90*time.Second); err != nil {
				return err
			}
			doc, err := browser.Text(p)
			if err != nil {
				return err
			}
			children := sitemap.Locs(doc)
			fmt.Printf("index lists %d sitemaps\n", len(children))
			dir := filepath.Join(cfg.DataDir, "sitemap")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			for i, u := range children {
				if limit > 0 && i >= limit {
					break
				}
				name := filepath.Join(dir, strings.TrimSuffix(filepath.Base(u), ".xml")+".txt")
				if stat, err := os.Stat(name); err == nil && stat.Size() > 0 {
					fmt.Printf("skip %s (exists)\n", filepath.Base(name))
					continue
				}
				if err := s.Navigate(p, u, 60*time.Second); err != nil {
					return err
				}
				doc, err := browser.Text(p)
				if err != nil {
					return err
				}
				locs := sitemap.Locs(doc)
				if err := os.WriteFile(name, []byte(strings.Join(locs, "\n")+"\n"), 0o644); err != nil {
					return err
				}
				n, counts, err := loader.load(cmd.Context(), locs)
				if err != nil {
					return err
				}
				fmt.Printf("%s: %d urls %v, %d queued\n", filepath.Base(u), len(locs), counts, n)
				time.Sleep(700 * time.Millisecond)
			}
			return nil
		},
	}
	fetch.Flags().IntVar(&limit, "limit", 0, "only fetch the first N child sitemaps (0 = all)")
	load := &cobra.Command{
		Use:   "load [dir]",
		Short: "Load DATA_DIR/sitemap/*.txt into the crawl queue (idempotent)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			dir := filepath.Join(cfg.DataDir, "sitemap")
			if len(args) == 1 {
				dir = args[0]
			}
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			loader, err := newQueueLoader(cmd.Context(), st)
			if err != nil {
				return err
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
			if len(files) == 0 {
				return fmt.Errorf("no sitemap files in %s (run `georges sitemap fetch` first)", dir)
			}
			var total int64
			all := map[sitemap.Kind]int{}
			for _, f := range files {
				b, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				n, counts, err := loader.load(cmd.Context(), strings.Split(strings.TrimSpace(string(b)), "\n"))
				if err != nil {
					return err
				}
				total += n
				for k, v := range counts {
					all[k] += v
				}
				fmt.Printf("%s: %d queued\n", filepath.Base(f), n)
			}
			fmt.Printf("%d files, %v, %d new queue rows\n", len(files), all, total)
			return nil
		},
	}
	cmd.AddCommand(fetch, load)
	return cmd
}

// queueLoader classifies sitemap URLs and enqueues product/brand/category pages (never images:
// the old site's pictures are not used).
type queueLoader struct {
	st    *store.Store
	match *site.BrandMatcher
}

func newQueueLoader(ctx context.Context, st *store.Store) (*queueLoader, error) {
	brands, err := st.ListBrands(ctx)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(brands))
	for _, b := range brands {
		slugs = append(slugs, b.Slug)
	}
	return &queueLoader{st: st, match: site.NewBrandMatcher(slugs)}, nil
}

func (q *queueLoader) load(ctx context.Context, urls []string) (int64, map[sitemap.Kind]int, error) {
	counts := map[sitemap.Kind]int{}
	var items []store.QueueItem
	var total int64
	flush := func() error {
		n, err := q.st.EnqueueCrawl(ctx, items)
		total += n
		items = items[:0]
		return err
	}
	for _, u := range urls {
		u = site.FixURL(u)
		if u == "" {
			continue
		}
		k := sitemap.Classify(u)
		counts[k]++
		if k == sitemap.KindImage || k == sitemap.KindOther {
			continue
		}
		it := store.QueueItem{URL: u, Kind: string(k)}
		if k == sitemap.KindProduct {
			it.BrandSlug, _, _ = q.match.Match(u)
		} else {
			it.Priority = 1
		}
		items = append(items, it)
		if len(items) >= 5000 {
			if err := flush(); err != nil {
				return total, counts, err
			}
		}
	}
	return total, counts, flush()
}
