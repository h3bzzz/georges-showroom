package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"georges/internal/db"
)

func dbCmd() *cobra.Command {
	c := &cobra.Command{Use: "db", Short: "Postgres: embedded server, migrations, queries"}
	c.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Run the embedded development Postgres until Ctrl-C",
			RunE: func(cmd *cobra.Command, args []string) error {
				stop, err := db.LaunchEmbedded(cfg.DatabaseURL, cfg.DataDir)
				if err != nil {
					return err
				}
				fmt.Println("embedded postgres listening; databases georges (v1) and georges2 (v2)")
				<-cmd.Context().Done()
				return stop()
			},
		},
		&cobra.Command{
			Use:   "migrate",
			Short: "Create the v2 database if needed and apply migrations",
			RunE: func(cmd *cobra.Command, args []string) error {
				pool, err := openDB(cmd.Context(), 2)
				if err != nil {
					return err
				}
				pool.Close()
				fmt.Println("migrations applied")
				return nil
			},
		},
		&cobra.Command{
			Use:   "sql QUERY",
			Short: "Run a read-only query and print the rows",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				pool, err := openDB(cmd.Context(), 2)
				if err != nil {
					return err
				}
				defer pool.Close()
				return runSQL(cmd.Context(), pool, args[0])
			},
		},
		&cobra.Command{
			Use:   "exec STATEMENT",
			Short: "Run one write statement (maintenance)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				pool, err := openDB(cmd.Context(), 2)
				if err != nil {
					return err
				}
				defer pool.Close()
				tag, err := pool.Exec(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				fmt.Println(tag.String())
				return nil
			},
		},
		&cobra.Command{
			Use:   "stats",
			Short: "Row counts of the main tables",
			RunE: func(cmd *cobra.Command, args []string) error {
				pool, err := openDB(cmd.Context(), 2)
				if err != nil {
					return err
				}
				defer pool.Close()
				var q []string
				for _, t := range []string{"brands", "families", "items", "assets", "asset_links", "family_media", "documents", "catalogs", "catalog_pages", "search_docs"} {
					q = append(q, fmt.Sprintf("SELECT '%s' AS table_name, count(*) AS rows FROM %s", t, t))
				}
				return runSQL(cmd.Context(), pool, strings.Join(q, " UNION ALL "))
			},
		},
	)
	return c
}

// runSQL prints a query's rows as a tab-separated table inside a read-only transaction.
func runSQL(ctx context.Context, pool *pgxpool.Pool, q string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	var names []string
	for _, f := range rows.FieldDescriptions() {
		names = append(names, f.Name)
	}
	fmt.Fprintln(tw, strings.Join(names, "\t"))
	n := 0
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
				cells[i] = "∅"
			case []byte:
				cells[i] = fmt.Sprintf("<%d bytes>", len(x))
			default:
				s := fmt.Sprint(v)
				if len(s) > 120 {
					s = s[:117] + "..."
				}
				cells[i] = s
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
		n++
	}
	tw.Flush()
	fmt.Printf("(%d rows)\n", n)
	return rows.Err()
}
