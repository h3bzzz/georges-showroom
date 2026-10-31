package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges_/internal/web"
)

func serveCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the new storefront",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := openOrStartDB(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.Migrate(cmd.Context()); err != nil {
				return err
			}
			fmt.Printf("George's storefront on http://localhost%s\n", addr)
			return web.Run(cmd.Context(), addr, web.New(st, cfg.DataDir))
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	return cmd
}
