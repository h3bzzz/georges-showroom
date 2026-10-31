package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/ocr"
	"georges_/internal/store"
	"georges_/internal/toc"
)

func tocDebugCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "toc-debug CATALOG_ID PAGE",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			id, _ := strconv.Atoi(args[0])
			pg, _ := strconv.Atoi(args[1])
			c, err := st.CatalogByID(cmd.Context(), id)
			if err != nil || c == nil {
				return fmt.Errorf("no catalog %d", id)
			}
			path := pageImagePath(cfg.DataDir, store.PageRef{CatalogID: id, BrandCode: c.BrandCode, Name: c.Name, Page: pg})
			b, err := os.ReadFile(strings.TrimSuffix(path, ".jpg") + ".words.json")
			if err != nil {
				return err
			}
			var words []ocr.Word
			_ = json.Unmarshal(b, &words)
			text := ocrText(words)
			fmt.Printf("contents page: %v\n--- text ---\n%s\n--- entries ---\n", toc.IsContentsPage(text), text)
			for _, e := range toc.ParsePrinted(text) {
				fmt.Printf("%-40s group=%-10s printed=%d\n", e.Title, e.Group, e.Printed)
			}
			return nil
		},
	}
}
