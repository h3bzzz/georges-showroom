package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"georges_/internal/store"
)

func dbSQLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sql QUERY",
		Short: "Run a read-only SQL query and print rows (debugging)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			st, err := store.Open(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.Pool.Query(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			defer rows.Close()
			var cols []string
			for _, f := range rows.FieldDescriptions() {
				cols = append(cols, f.Name)
			}
			fmt.Println(strings.Join(cols, " | "))
			n := 0
			for rows.Next() {
				vals, err := rows.Values()
				if err != nil {
					return err
				}
				var out []string
				for _, v := range vals {
					out = append(out, fmt.Sprint(v))
				}
				fmt.Println(strings.Join(out, " | "))
				n++
			}
			fmt.Printf("(%d rows)\n", n)
			return rows.Err()
		},
	}
}
