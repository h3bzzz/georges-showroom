package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/site"
	"georges_/internal/store"
	"georges_/internal/wayback"
)

func waybackCmd() *cobra.Command {
	var limit, workers int
	var rps float64
	var list string
	cmd := &cobra.Command{
		Use:   "wayback",
		Short: "Bootstrap product records from archived product pages (structured data only, never images)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			wb := wayback.New(rps)
			var snaps []wayback.Snapshot
			if list != "" {
				snaps, err = wayback.ListFromFile(list)
			} else {
				snaps, err = wb.ListProducts(cmd.Context(), limit)
			}
			if err != nil {
				return err
			}
			if limit > 0 && len(snaps) > limit {
				snaps = snaps[:limit]
			}
			fmt.Printf("%d archived product pages\n", len(snaps))
			var done, ok, skipped int64
			start := time.Now()
			jobs := make(chan wayback.Snapshot)
			var wg sync.WaitGroup
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for s := range jobs {
						out := productJSONPath(cfg.DataDir, s.URL)
						if _, err := os.Stat(out); err == nil {
							atomic.AddInt64(&skipped, 1)
							continue
						}
						html, err := wb.Fetch(cmd.Context(), s)
						if err != nil {
							fmt.Printf("FAIL %s: %v\n", s.URL, err)
							continue
						}
						p, err := site.ParseProduct(strings.NewReader(string(html)), s.URL)
						if err != nil {
							continue
						}
						p.Images = nil // never carry old-site imagery
						_ = os.MkdirAll(filepath.Dir(out), 0o755)
						b, _ := json.MarshalIndent(p, "", "  ")
						_ = os.WriteFile(out, b, 0o644)
						if _, err := st.UpsertProduct(cmd.Context(), p); err != nil {
							fmt.Printf("FAIL db %s: %v\n", p.SKU, err)
							continue
						}
						n := atomic.AddInt64(&ok, 1)
						if n%100 == 0 {
							fmt.Printf("  %d ok, %d skipped, %s\n", n, atomic.LoadInt64(&skipped), time.Since(start).Round(time.Second))
						}
					}
				}()
			}
			for _, s := range snaps {
				select {
				case jobs <- s:
					done++
				case <-cmd.Context().Done():
					close(jobs)
					wg.Wait()
					return cmd.Context().Err()
				}
			}
			close(jobs)
			wg.Wait()
			fmt.Printf("wayback done: %d ok, %d skipped of %d in %s\n", ok, skipped, done, time.Since(start).Round(time.Second))
			if n, err := st.ReassignRooms(cmd.Context()); err == nil {
				fmt.Printf("rooms assigned for %d families\n", n)
			}
			if n, err := st.LinkCrops(cmd.Context()); err == nil {
				fmt.Printf("%d family↔crop links\n", n)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max snapshots (0 = all)")
	cmd.Flags().StringVar(&list, "list", "", "CDX text file to use instead of querying archive.org")
	cmd.Flags().IntVar(&workers, "workers", 3, "parallel fetches")
	cmd.Flags().Float64Var(&rps, "rps", 4, "requests per second to archive.org")
	return cmd
}
