package main

import (
	"log/slog"

	"github.com/spf13/cobra"

	"georges/internal/chat"
	"georges/internal/httpx"
	"georges/internal/store"
	"georges/internal/web"
)

func serveCmd() *cobra.Command {
	var addr string
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the storefront",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 12)
			if err != nil {
				return err
			}
			defer pool.Close()
			if addr == "" {
				addr = cfg.Addr
			}
			st := store.New(pool)
			assistant := chat.New(cfg.AnthropicAPIKey, cfg.ChatModel, st)
			if assistant.Enabled() {
				if m, err := openMatcher(cmd.Context(), pool); err == nil {
					assistant.Matcher = m
				} else {
					slog.Warn("assistant runs without the picture matcher", "err", err)
				}
			} else {
				slog.Info("assistant disabled: set ANTHROPIC_API_KEY in .env to enable it")
			}
			srv := web.New(st, cfg.DataDir, cfg.PublicURL, cfg.Dev(), assistant)
			return httpx.Serve(cmd.Context(), addr, srv)
		},
	}
	c.Flags().StringVar(&addr, "addr", "", "listen address (default $ADDR or :8080)")
	return c
}
