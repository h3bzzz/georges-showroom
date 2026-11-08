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

// FinishTable is one brand's learned mapping: "position:token" → finish name. A brand may
// carry the finish in different places for different lines (Ashley Norton: first token on
// door hardware, last on cabinet hardware), so tokens are accepted per position.
type FinishTable struct {
	Brand  string
	Codes  map[string]string // "first:WL", "second:112", "last:PC"
	Taught int
}

// positions: separated tokens first; then glued suffixes (Emtek "3051MELHUS26" → US26,
// "7058BZEGMB" → MB), longest first so US10B wins over 0B.
var positions = []string{"first", "second", "last", "suffix5", "suffix4", "suffix3", "suffix2"}

// position returns the token at a position ("" when the SKU does not have one).
func position(sku, pos string) string {
	u := strings.ToUpper(strings.TrimSpace(sku))
	if strings.HasPrefix(pos, "suffix") {
		n := int(pos[len(pos)-1] - '0')
		if len(u) <= n+2 {
			return ""
		}
		suf := u[len(u)-n:]
		// a glued code starts with a letter and follows a digit or letter that is not part
		// of it; purely numeric tails are sizes
		if suf[0] < 'A' || suf[0] > 'Z' || isDigits(suf) {
			return ""
		}
		return suf
	}
	toks := reTok.Split(u, -1)
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

// Lookup finds the finish a SKU's tokens name, first position wins.
func (t *FinishTable) Lookup(sku string) (code, finish string) {
	for _, pos := range positions {
		tok := position(sku, pos)
		if tok == "" {
			continue
		}
		if f, ok := t.Codes[pos+":"+tok]; ok {
			return tok, f
		}
	}
	return "", ""
}

// Learn builds the table for one brand from feed rows that name a finish. A token at a
// position is accepted as a finish code when at least five rows carry it and nine in ten of
// them agree on the finish; a function or size code fails that test because it appears with
// every finish.
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
	tbl := &FinishTable{Brand: brand, Codes: map[string]string{}, Taught: len(pairs)}
	for _, pos := range positions {
		counts := map[string]map[string]int{}
		for _, p := range pairs {
			t := position(p.sku, pos)
			// a finish code is short; long digit runs are sizes or model numbers
			if t == "" || len(t) > 6 || (len(t) > 3 && isDigits(t)) {
				continue
			}
			if counts[t] == nil {
				counts[t] = map[string]int{}
			}
			counts[t][p.finish]++
		}
		for tok, fin := range counts {
			n := 0
			bestF, bestN := "", 0
			for f, c := range fin {
				n += c
				if c > bestN || c == bestN && f < bestF {
					bestF, bestN = f, c
				}
			}
			need := 5
			if strings.HasPrefix(pos, "suffix") {
				need = 25 // a glued tail must prove itself on more rows
			}
			if n >= need && float64(bestN) >= 0.9*float64(n) {
				tbl.Codes[pos+":"+tok] = bestF
			}
		}
	}
	if len(tbl.Codes) == 0 {
		return nil, nil
	}
	return tbl, nil
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
			if t, f := tbl.Lookup(sku); f != "" {
				ups = append(ups, upd{id, t, f})
			}
		}
		ir.Close()
		slog.Info("finish codes", "brand", code, "codes", len(tbl.Codes), "taught_by", tbl.Taught, "blank_items", blank, "fill", len(ups))
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
					fmt.Printf("  %s %-14s → %s\n", code, k, tbl.Codes[k])
				}
			}
			filled += len(ups)
			continue
		}
		batch := &pgx.Batch{}
		for key, f := range tbl.Codes {
			tok := key[strings.IndexByte(key, ':')+1:]
			batch.Queue(`INSERT INTO finish_codes (brand_code, code, finish) VALUES ($1,$2,$3) ON CONFLICT (brand_code, code) DO NOTHING`, code, tok, f)
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
