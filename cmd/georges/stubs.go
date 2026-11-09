package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges/internal/media"
	"georges/internal/search"
)

func mediaCmd() *cobra.Command {
	c := &cobra.Command{Use: "media", Short: "Pick each family's card and gallery pictures"}
	c.AddCommand(&cobra.Command{
		Use:   "pick",
		Short: "Rebuild family_media from asset_links and asset measures",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := (&media.Picker{Pool: pool}).Run(cmd.Context())
			fmt.Printf("%d media rows\n", n)
			return err
		},
	})
	var brand string
	link := &cobra.Command{
		Use:   "link-catalog",
		Short: "Link cropped catalog photographs to the SKUs printed nearest to them (families with no catalog or maker picture yet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := media.LinkCatalog(cmd.Context(), pool, brand)
			fmt.Printf("%d catalog links written; run `georges media pick` then `georges index build`\n", n)
			return err
		},
	}
	link.Flags().StringVar(&brand, "brand", "", "one brand code")
	c.AddCommand(link)
	return c
}

func indexCmd() *cobra.Command {
	c := &cobra.Command{Use: "index", Short: "Rebuild the search index"}
	c.AddCommand(&cobra.Command{
		Use:   "build",
		Short: "Rebuild search_docs",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := (&search.Indexer{Pool: pool}).Run(cmd.Context())
			fmt.Printf("%d docs\n", n)
			return err
		},
	})
	return c
}
