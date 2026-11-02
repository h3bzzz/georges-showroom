package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"georges_/internal/store"
)

// catalogsRejectCmd is the hand brake of the image-QA loop: a crop seen on a page that should
// never be a card (a wrong product, a mislabelled subject, a garbled upscale, a diagram the
// model reads as a photo) is refused by path and every family that showed it is re-picked.
func catalogsRejectCmd() *cobra.Command {
	var noRepick bool
	cmd := &cobra.Command{
		Use:   "reject PATH...",
		Short: "Refuse crops as card pictures (crop paths, HD paths, /files/ or /img/{w}/ URLs) and re-pick family photos",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.RejectCrops(cmd.Context(), args)
			if err != nil {
				return err
			}
			fmt.Printf("%d crops rejected\n", n)
			if n == 0 || noRepick {
				return nil
			}
			if err := st.RefreshFamilyBest(cmd.Context()); err != nil {
				return err
			}
			_, err = st.PromoteHD(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&noRepick, "no-repick", false, "only flag; skip the family photo re-pick")
	return cmd
}
