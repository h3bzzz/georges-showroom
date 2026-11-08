package feed

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Finish codes are learned from the feed itself: within a brand, the rows that name their
// finish teach which SKU token carries it (Ashley Norton's first token "WL" = White Bronze,
// Baldwin's second ".112" = Venetian Bronze, Brizo's last "-PC" = Polished Chrome). The
// learned table fills the finish of every item the feed left blank and feeds finish_codes,
// which the picker and the product page use to say which finish a picture shows.

var reTok = regexp.MustCompile(`[.\-/ ]+`)

// FinishTable is one brand's learned mapping.
type FinishTable struct {
	Brand       string
	Position    string // first | second | last
	Codes       map[string]string
	Consistency float64 // share of teaching rows whose token agreed with the majority finish
	Taught      int
}

// position returns the token at a position ("" when the SKU is too short).
func position(sku, pos string) string {
	toks := reTok.Split(strings.ToUpper(strings.TrimSpace(sku)), -1)
	switch {
	case len(toks) < 2:
		return ""
	case pos == "first":
		return toks[0]
	case pos == "second":
		if len(toks) < 3 {
			return ""
		}
		return toks[1]
	default:
		return toks[len(toks)-1]
	}
}

// Learn builds the table for one brand from feed rows that name a finish.
func Learn(ctx context.Context, pool *pgxpool.Pool, brand string) (*FinishTable, error) {
	rows, err := pool.Query(ctx, `SELECT sku, finish FROM feed_products WHERE brand_code=$1 AND finish IS NOT NULL AND finish <> ''`, brand)
	if err != nil {
		return nil, err
	}
	type pair struct{ sku, finish string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.sku, &p.finish) == nil {
			pairs = append(pairs, p)
		}
	}
	rows.Close()
	if len(pairs) < 20 {
		return nil, nil
	}
	var best *FinishTable
	for _, pos := range []string{"first", "second", "last"} {
		counts := map[string]map[string]int{}
		for _, p := range pairs {
			t := position(p.sku, pos)
			// a finish code is short and not purely numeric-with-length-of-a-size; Baldwin's
			// three-digit codes are the exception, so digits are allowed up to 3
			if t == "" || len(t) > 6 || (len(t) > 3 && isDigits(t)) {
				continue
			}
			if counts[t] == nil {
				counts[t] = map[string]int{}
			}
			counts[t][p.finish]++
		}
		tbl := &FinishTable{Brand: brand, Position: pos, Codes: map[string]string{}}
		agree, total := 0, 0
		for tok, fin := range counts {
			n := 0
			bestF, bestN := "", 0
			for f, c := range fin {
				n += c
				if c > bestN || c == bestN && f < bestF {
					bestF, bestN = f, c
				}
			}
			if n < 3 {
				continue
			}
			total += n
			agree += bestN
			if float64(bestN) >= 0.8*float64(n) {
				tbl.Codes[tok] = bestF
			}
		}
		if total == 0 {
			continue
		}
		tbl.Consistency = float64(agree) / float64(total)
		tbl.Taught = total
		// a real finish position explains most rows consistently and uses a small vocabulary
		if tbl.Consistency < 0.9 || total < len(pairs)/2 || len(tbl.Codes) > 400 {
			continue
		}
		if best == nil || tbl.Consistency > best.Consistency || tbl.Consistency == best.Consistency && len(tbl.Codes) > len(best.Codes) {
			best = tbl
		}
	}
	return best, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// ApplyFinishes learns every brand (or one) and fills items.finish / finish_code where blank,
// writes finish_codes, and refreshes families.finishes. Returns items filled.
func ApplyFinishes(ctx context.Context, pool *pgxpool.Pool, brand string, dry bool) (int, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT brand_code FROM feed_products WHERE $1='' OR brand_code=$1 ORDER BY 1`, brand)
	if err != nil {
		return 0, err
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	filled := 0
	for _, code := range codes {
		tbl, err := Learn(ctx, pool, code)
		if err != nil {
			return filled, err
		}
		if tbl == nil {
			continue
		}
		var brandID int
		if err := pool.QueryRow(ctx, `SELECT id FROM brands WHERE code=$1`, code).Scan(&brandID); err != nil {
			continue
		}
		// items of the brand with no finish
		ir, err := pool.Query(ctx, `SELECT id, sku FROM items WHERE brand_id=$1 AND (finish IS NULL OR finish='')`, brandID)
		if err != nil {
			return filled, err
		}
		type upd struct {
			id          int64
			tok, finish string
		}
		var ups []upd
		blank := 0
		for ir.Next() {
			var id int64
			var sku string
			if ir.Scan(&id, &sku) != nil {
				continue
			}
			blank++
			t := position(sku, tbl.Position)
			if f, ok := tbl.Codes[t]; ok {
				ups = append(ups, upd{id, t, f})
			}
		}
		ir.Close()
		slog.Info("finish codes", "brand", code, "position", tbl.Position, "codes", len(tbl.Codes), "consistency", fmt.Sprintf("%.2f", tbl.Consistency), "blank_items", blank, "fill", len(ups))
		if dry {
			if len(ups) > 0 {
				keys := make([]string, 0, len(tbl.Codes))
				for k := range tbl.Codes {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				if len(keys) > 12 {
					keys = keys[:12]
				}
				for _, k := range keys {
					fmt.Printf("  %s %-8s → %s\n", code, k, tbl.Codes[k])
				}
			}
			filled += len(ups)
			continue
		}
		batch := &pgx.Batch{}
		for tok, f := range tbl.Codes {
			batch.Queue(`INSERT INTO finish_codes (brand_code, code, finish) VALUES ($1,$2,$3) ON CONFLICT (brand_code, code) DO UPDATE SET finish=EXCLUDED.finish`, code, tok, f)
		}
		for _, u := range ups {
			batch.Queue(`UPDATE items SET finish=$2, finish_code=$3 WHERE id=$1`, u.id, u.finish, u.tok)
		}
		br := pool.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return filled, fmt.Errorf("%s: %w", code, err)
			}
		}
		if err := br.Close(); err != nil {
			return filled, err
		}
		if len(ups) > 0 {
			if _, err := pool.Exec(ctx, `
				UPDATE families f SET finishes = x.fins FROM (
				  SELECT i.family_id, array_agg(DISTINCT i.finish ORDER BY i.finish) AS fins
				  FROM items i WHERE i.brand_id=$1 AND i.finish IS NOT NULL AND i.finish <> '' GROUP BY i.family_id) x
				WHERE x.family_id = f.id AND f.finishes IS DISTINCT FROM x.fins`, brandID); err != nil {
				return filled, err
			}
		}
		filled += len(ups)
	}
	return filled, nil
}
