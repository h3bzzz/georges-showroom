package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"georges/internal/db"
	"georges/internal/legacy"
)

func importCmd() *cobra.Command {
	var only string
	c := &cobra.Command{
		Use:   "import legacy",
		Short: "Import the v1 database (LEGACY_DATABASE_URL) into v2",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "legacy" {
				return fmt.Errorf("unknown source %q", args[0])
			}
			ctx := cmd.Context()
			dst, err := openDB(ctx, 8)
			if err != nil {
				return err
			}
			defer dst.Close()
			src, err := db.Open(ctx, cfg.LegacyDatabaseURL, 4)
			if err != nil {
				return fmt.Errorf("%w: %v", legacy.ErrNoLegacy, err)
			}
			defer src.Close()
			// an error mid-import must surface before the deferred pool closes wait on open cursors
			im := &legacy.Importer{Src: src, Dst: dst, DataDir: cfg.DataDir, Only: map[string]bool{}}
			for _, s := range strings.Split(only, ",") {
				if s = strings.TrimSpace(s); s != "" {
					im.Only[s] = true
				}
			}
			if err := im.Run(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVar(&only, "only", "", "comma-separated steps: brands,catalogs,families,items,documents,skus,assets,links,covers")
	return c
}
