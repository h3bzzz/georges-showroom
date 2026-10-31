package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges_/internal/store"
)

// catalogsDedupeCmd marks crops that repeat the same photograph within a brand (the collection
// shot that opens three price lists) so galleries and the upscaler show each shot once.
func catalogsDedupeCmd() *cobra.Command {
	var brand string
	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Mark duplicate crops of one photograph across a brand's books (CLIP cosine on stored embeddings)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.DedupeCrops(cmd.Context(), brand)
			if err != nil {
				return err
			}
			fmt.Printf("%d crops marked as duplicates of a larger copy\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	return cmd
}
