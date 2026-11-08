// Package report prints the numbers that say how complete the catalog is: per brand, how many
// designs have a real picture, a price, finish names and a readable title. It is the scoreboard
// for the "photo and name on every design" work.
package report

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Row is one brand's coverage.
type Row struct {
	Code, Name                         string
	Families, Pictured, Vendor, Priced int
	Finished, Titled                   int
}

// Coverage computes the rows (tier 0 and 1 families only: fixtures and ordinary products).
func Coverage(ctx context.Context, pool *pgxpool.Pool, brand string) ([]Row, Row, error) {
	rows, err := pool.Query(ctx, `
		SELECT COALESCE(b.code,''), b.name, count(*),
		       count(*) FILTER (WHERE sd.card_hash IS NOT NULL),
		       count(*) FILTER (WHERE EXISTS (SELECT 1 FROM family_media fm JOIN assets a ON a.id=fm.asset_id WHERE fm.family_id=f.id AND fm.role='card' AND a.source='vendor')),
		       count(*) FILTER (WHERE sd.price_cents IS NOT NULL),
		       count(*) FILTER (WHERE cardinality(f.finishes) > 0),
		       count(*) FILTER (WHERE f.display_title IS NOT NULL OR (f.title ~ '[a-z]{3}' AND f.title !~ '^[A-Z0-9 ./-]+$'))
		FROM families f JOIN brands b ON b.id=f.brand_id JOIN search_docs sd ON sd.family_id=f.id
		WHERE f.tier < 2 AND ($1 = '' OR b.code = $1)
		GROUP BY 1, 2 ORDER BY 3 DESC`, brand)
	if err != nil {
		return nil, Row{}, err
	}
	defer rows.Close()
	var out []Row
	var total Row
	total.Name = "all brands"
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Code, &r.Name, &r.Families, &r.Pictured, &r.Vendor, &r.Priced, &r.Finished, &r.Titled); err != nil {
			return nil, Row{}, err
		}
		out = append(out, r)
		total.Families += r.Families
		total.Pictured += r.Pictured
		total.Vendor += r.Vendor
		total.Priced += r.Priced
		total.Finished += r.Finished
		total.Titled += r.Titled
	}
	return out, total, rows.Err()
}

// Print writes the table.
func Print(w io.Writer, rows []Row, total Row, limit int) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "brand\tdesigns\tpictured\tmaker photo\tpriced\tfinishes\treadable title\t")
	pct := func(n, of int) string {
		if of == 0 {
			return "-"
		}
		return fmt.Sprintf("%d%%", n*100/of)
	}
	for i, r := range rows {
		if limit > 0 && i >= limit {
			break
		}
		fmt.Fprintf(tw, "%s %s\t%d\t%s\t%s\t%s\t%s\t%s\t\n", r.Code, r.Name, r.Families, pct(r.Pictured, r.Families), pct(r.Vendor, r.Families), pct(r.Priced, r.Families), pct(r.Finished, r.Families), pct(r.Titled, r.Families))
	}
	fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t\n", total.Name, total.Families, pct(total.Pictured, total.Families), pct(total.Vendor, total.Families), pct(total.Priced, total.Families), pct(total.Finished, total.Families), pct(total.Titled, total.Families))
	tw.Flush()
}
