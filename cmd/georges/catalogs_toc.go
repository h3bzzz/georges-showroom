package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/ocr"
	"georges_/internal/store"
	"georges_/internal/toc"
)

// dictPath is the word list toc.Fold uses to tell a lone English heading from an OCR misread:
// dwyl/english-words (370k words) fetched to data/raw/tools/words, or the system list.
func dictPath(dataDir string) string {
	for _, p := range []string{filepath.Join(dataDir, "tools", "words", "words_alpha.txt"), "/usr/share/dict/words"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func catalogsTocCmd() *cobra.Command {
	var brand string
	cmd := &cobra.Command{
		Use:   "toc",
		Short: "Build a table of contents for every OCR'd catalog from page headings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			refs, err := st.OCRPages(cmd.Context(), brand)
			if err != nil {
				return err
			}
			byCat := map[int]map[int]string{}
			pagesOf := map[int]int{}
			texts := map[int]map[int]string{}
			for _, p := range refs {
				path := pageImagePath(cfg.DataDir, p)
				b, err := os.ReadFile(strings.TrimSuffix(path, ".jpg") + ".words.json")
				if err != nil {
					continue
				}
				var words []ocr.Word
				if json.Unmarshal(b, &words) != nil {
					continue
				}
				if byCat[p.CatalogID] == nil {
					byCat[p.CatalogID] = map[int]string{}
				}
				pagesOf[p.CatalogID] = p.Pages
				byCat[p.CatalogID][p.Page] = toc.PageHeading(words, 2200)
				if p.Page <= 60 {
					if texts[p.CatalogID] == nil {
						texts[p.CatalogID] = map[int]string{}
					}
					texts[p.CatalogID][p.Page] = ocrText(words)
				}
			}
			total, printed := 0, 0
			dict := toc.LoadDict(dictPath(cfg.DataDir))
			codeOf := map[int]string{}
			for _, p := range refs {
				codeOf[p.CatalogID] = p.BrandCode
			}
			for id, heads := range byCat {
				bname, cols, _ := st.BrandCollections(cmd.Context(), codeOf[id])
				entries := toc.Fold(heads, pagesOf[id], toc.Options{Brand: bname, Collections: cols, Dict: dict})
				var rows []store.TOCEntry
				for _, e := range entries {
					rows = append(rows, store.TOCEntry{Page: e.Page, Title: e.Title, Level: e.Level})
				}
				if err := st.ReplaceTOC(cmd.Context(), id, rows, "ocr"); err != nil {
					return err
				}
				total += len(rows)
				if pr := printedTOC(texts[id], heads, pagesOf[id]); len(pr) > 0 {
					if err := st.ReplaceTOC(cmd.Context(), id, pr, "printed"); err != nil {
						return err
					}
					printed += len(pr)
				}
			}
			fmt.Printf("toc: %d catalogs, %d detected entries, %d printed entries\n", len(byCat), total, printed)
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	return cmd
}

// ocrText rebuilds line text from cached words (same grouping tesseract used).
func ocrText(words []ocr.Word) string {
	var b strings.Builder
	last := -1
	for _, w := range words {
		key := w.Block*100000 + w.Par*1000 + w.Line
		if key != last && b.Len() > 0 {
			b.WriteByte('\n')
		} else if b.Len() > 0 {
			b.WriteByte(' ')
		}
		last = key
		b.WriteString(w.Text)
	}
	return b.String()
}

// printedTOC finds contents pages in the first 60 pages, parses them and converts printed
// page numbers to book page indexes using the offset that best agrees with detected headings.
func printedTOC(texts map[int]string, heads map[int]string, pages int) []store.TOCEntry {
	var all []toc.Printed
	seen := map[int]bool{}
	for p := 1; p <= 60; p++ {
		t, ok := texts[p]
		if !ok || seen[p] || !toc.IsContentsPage(t) {
			continue
		}
		seen[p] = true
		all = append(all, toc.ParsePrinted(t)...)
		// continuation pages ("Bath (cont.)") directly after
		for q := p + 1; q <= p+3; q++ {
			if !seen[q] && strings.Contains(strings.ToLower(texts[q]), "(cont") {
				seen[q] = true
				all = append(all, toc.ParsePrinted(texts[q])...)
			}
		}
	}
	// drop OCR junk: too few letters, or a lone short word
	var kept []toc.Printed
	for _, e := range all {
		e.Title = toc.CleanTitle(e.Title)
		letters := 0
		for _, r := range e.Title {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				letters++
			}
		}
		words := len(strings.Fields(e.Title))
		if letters < 4 || (words == 1 && letters < 6) || strings.HasPrefix(e.Title, ")") {
			continue
		}
		kept = append(kept, e)
	}
	all = kept
	if len(all) < 3 {
		return nil
	}
	off, votes := toc.Offset(all, heads, pages)
	if votes < 2 {
		off = 0
	}
	var rows []store.TOCEntry
	lastGroup := ""
	for _, e := range all {
		page := e.Printed + off
		if page < 1 || page > pages {
			continue
		}
		if e.Group != "" && e.Group != lastGroup {
			rows = append(rows, store.TOCEntry{Page: page, Title: e.Group, Level: 1})
			lastGroup = e.Group
		}
		rows = append(rows, store.TOCEntry{Page: page, Title: e.Title, Level: 2})
	}
	return rows
}
