package main

import (
	"github.com/spf13/cobra"

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
			srv := web.New(store.New(pool), cfg.DataDir, cfg.PublicURL, cfg.Dev())
			return httpx.Serve(cmd.Context(), addr, srv)
		},
	}
	c.Flags().StringVar(&addr, "addr", "", "listen address (default $ADDR or :8080)")
	return c
}
