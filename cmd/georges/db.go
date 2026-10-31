package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges_/internal/store"
)

func dbCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Postgres: embedded server, migrations, stats"}
	start := &cobra.Command{
		Use:   "start",
		Short: "Run an embedded Postgres (downloads binaries once; no root) matching DATABASE_URL; blocks until Ctrl-C",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			return store.StartEmbedded(cmd.Context(), cfg.DatabaseURL, cfg.DataDir)
		},
	}
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Apply schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Migrate(cmd.Context()); err != nil {
				return err
			}
			fmt.Println("migrations applied")
			return nil
		},
	}
	stats := &cobra.Command{
		Use:   "stats",
		Short: "Row counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			b, f, p, c, pg, err := st.Counts(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("brands=%d families=%d products=%d catalogs=%d catalog_pages=%d\n", b, f, p, c, pg)
			return nil
		},
	}
	cmd.AddCommand(start, migrate, stats, dbSQLCmd())
	return cmd
}
