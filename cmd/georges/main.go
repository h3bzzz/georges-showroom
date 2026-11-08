// Command georges is George's Showroom v2: the storefront server and the offline jobs that
// feed it (import, asset build, media pick, index build).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"georges/internal/config"
	"georges/internal/db"
)

var cfg *config.Config

func main() {
	root := &cobra.Command{
		Use:           "georges",
		Short:         "George's Showroom v2: storefront and data jobs",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.Load()
			if err != nil {
				return err
			}
			cfg = c
			lvl := slog.LevelInfo
			if os.Getenv("DEBUG") != "" {
				lvl = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
			return nil
		},
	}
	root.AddCommand(dbCmd(), serveCmd(), importCmd(), assetsCmd(), mediaCmd(), indexCmd(), specsCmd(), vendorsCmd(), chatCmd(), visionCmd())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// openDB connects to the v2 database, creating it and applying migrations when needed. When
// nothing listens on the port in development it launches the embedded Postgres itself.
func openDB(ctx context.Context, maxConns int32) (*pgxpool.Pool, error) {
	if err := db.EnsureDatabase(ctx, cfg.DatabaseURL); err != nil {
		if !cfg.Dev() {
			return nil, err
		}
		slog.Info("database not reachable, starting the embedded Postgres", "err", err)
		if _, err := db.LaunchEmbedded(cfg.DatabaseURL, cfg.DataDir); err != nil {
			return nil, err
		}
		if err := db.EnsureDatabase(ctx, cfg.DatabaseURL); err != nil {
			return nil, err
		}
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL, maxConns)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return pool, nil
}
