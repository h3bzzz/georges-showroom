package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/specs"
	"georges_/internal/store"
)

// specsSyncCmd downloads every spec/install sheet the crawl has seen (documents.path IS NULL),
// stores its text for search and attribute extraction, and pulls the embedded raster images
// out at native resolution as product_images source='spec' (manufacturer photography is
// usually sharper there than on any catalog page render).
func specsSyncCmd() *cobra.Command {
	var brand string
	var limit, minSide int
	var rps float64
	var retry, images, reimage bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch unfetched spec/install PDFs, extract text and embedded images (resumable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			var tasks []store.DocTask
			if reimage {
				tasks, err = st.DocumentsToImage(ctx, brand, limit)
			} else {
				tasks, err = st.DocumentsToFetch(ctx, brand, limit, retry)
			}
			if err != nil {
				return err
			}
			fmt.Printf("%d documents to process\n", len(tasks))
			cl := specs.NewClient(rps)
			docDir := filepath.Join(cfg.DataDir, "docs")
			start := time.Now()
			ok, failed, imgN := 0, 0, 0
			for i, t := range tasks {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				res, err := cl.Fetch(ctx, t.URL, docDir, true)
				if err != nil && res == nil {
					failed++
					_ = st.SaveDocumentError(ctx, t.ID, err.Error())
					fmt.Printf("FAIL %s: %v\n", t.URL, err)
					continue
				}
				rel, _ := filepath.Rel(cfg.DataDir, res.Path)
				if err := st.SaveDocument(ctx, t.ID, filepath.ToSlash(rel), res.SHA1, res.Text, res.Bytes, res.Pages); err != nil {
					return err
				}
				ok++
				if images {
					code := t.BrandCode
					if code == "" {
						code = brandCodeFromDocURL(res.URL)
					}
					out := filepath.Join(cfg.DataDir, "specimg", code, strconv.FormatInt(t.ID, 10))
					_ = os.RemoveAll(out)
					if err := st.ClearSpecImages(ctx, t.ID); err != nil {
						return err
					}
					imgs, err := specs.Images(ctx, res.Path, out, minSide)
					if err != nil {
						fmt.Printf("  images %s: %v\n", filepath.Base(res.Path), err)
					}
					if len(imgs) == 0 {
						_ = os.RemoveAll(out)
					}
					for n, im := range imgs {
						irel, _ := filepath.Rel(cfg.DataDir, im.Path)
						if err := st.AddSpecImage(ctx, t.ID, t.FamilyID, t.ProductID, n, filepath.ToSlash(irel), im.W, im.H); err != nil {
							return err
						}
						imgN++
					}
					_ = st.MarkDocumentImages(ctx, t.ID)
				}
				if (i+1)%25 == 0 {
					fmt.Printf("  %d/%d fetched, %d failed, %d images (%s)\n", ok, len(tasks), failed, imgN, time.Since(start).Round(time.Second))
				}
			}
			ds, _ := st.DocumentStats(ctx)
			fmt.Printf("done: %d fetched, %d failed, %d spec images in %s; totals: %d docs, %d fetched, %d with text, %d spec images\n",
				ok, failed, imgN, time.Since(start).Round(time.Second), ds.Total, ds.Fetched, ds.WithText, ds.SpecImages)
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().IntVar(&limit, "limit", 100000, "max documents this run")
	cmd.Flags().IntVar(&minSide, "min-side", 300, "keep embedded images at least this many px on the short side")
	cmd.Flags().Float64Var(&rps, "rps", 4, "requests per second")
	cmd.Flags().BoolVar(&retry, "retry", false, "retry documents that failed before")
	cmd.Flags().BoolVar(&images, "images", true, "extract embedded images with pdfimages")
	cmd.Flags().BoolVar(&reimage, "reimage", false, "re-extract images from documents already fetched (after a filter change)")
	return cmd
}

// brandCodeFromDocURL reads the brand code out of …/brands/<CODE>/documents/….
func brandCodeFromDocURL(u string) string {
	if i := strings.Index(u, "/brands/"); i >= 0 {
		rest := u[i+len("/brands/"):]
		if j := strings.Index(rest, "/"); j > 0 {
			return rest[:j]
		}
	}
	return "misc"
}
