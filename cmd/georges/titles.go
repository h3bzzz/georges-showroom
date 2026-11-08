package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges/internal/titles"
)

func titlesCmd() *cobra.Command {
	var brand string
	var dry bool
	c := &cobra.Command{Use: "titles", Short: "Display titles for families whose printed title is a model number or a shared heading"}
	build := &cobra.Command{
		Use:   "build",
		Short: "Recompute families.display_title (then run `index build`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			n, err := titles.Build(cmd.Context(), pool, brand, dry)
			fmt.Printf("%d titles changed\n", n)
			return err
		},
	}
	build.Flags().StringVar(&brand, "brand", "", "one brand code")
	build.Flags().BoolVar(&dry, "dry", false, "print the first changes, write nothing")
	c.AddCommand(build)
	return c
}
