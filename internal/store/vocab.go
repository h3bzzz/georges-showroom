package store

import (
	"context"
	"strings"
)

// RefreshVocab rebuilds search_vocab from every word the catalog prints: family titles,
// collections, categories, product types, finish names and brand names. Words shorter than
// three letters, pure numbers and words seen only once (OCR debris) are left out.
func (s *Store) RefreshVocab(ctx context.Context) (int64, error) {
	_, err := s.Pool.Exec(ctx, `
		CREATE TEMP TABLE vocab_new AS
		SELECT w AS word, count(*)::int AS n FROM (
		  SELECT regexp_split_to_table(lower(concat_ws(' ', f.title, f.collection, f.product_type, array_to_string(f.category_path,' '))), '[^a-z]+') AS w
		  FROM product_families f
		  UNION ALL
		  SELECT regexp_split_to_table(lower(p.finish), '[^a-z]+') FROM products p WHERE p.finish IS NOT NULL
		  UNION ALL
		  SELECT regexp_split_to_table(lower(b.name), '[^a-z]+') FROM brands b
		) x
		WHERE length(w) >= 3
		GROUP BY w HAVING count(*) >= 2`)
	if err != nil {
		return 0, err
	}
	defer s.Pool.Exec(ctx, `DROP TABLE IF EXISTS vocab_new`)
	if _, err := s.Pool.Exec(ctx, `DELETE FROM search_vocab v WHERE NOT EXISTS (SELECT 1 FROM vocab_new n WHERE n.word=v.word)`); err != nil {
		return 0, err
	}
	t, err := s.Pool.Exec(ctx, `INSERT INTO search_vocab (word, n) SELECT word, n FROM vocab_new
		ON CONFLICT (word) DO UPDATE SET n=EXCLUDED.n WHERE search_vocab.n <> EXCLUDED.n`)
	if err != nil {
		return 0, err
	}
	return t.RowsAffected(), nil
}

// SpellFix returns a replacement for every word the catalog does not know but is one typo
// away from a word it does ("brused" → "brushed"): trigram similarity ≥ 0.45 (0.55 for words
// under six letters), the commonest candidate on a tie. Known words, numbers and words
// shorter than four letters are left alone.
func (s *Store) SpellFix(ctx context.Context, words []string) map[string]string {
	var unknown []string
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) < 4 || strings.ContainsAny(w, "0123456789") {
			continue
		}
		unknown = append(unknown, w)
	}
	if len(unknown) == 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT word FROM search_vocab WHERE word = ANY($1)`, unknown)
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for rows.Next() {
		var w string
		if rows.Scan(&w) == nil {
			known[w] = true
		}
	}
	rows.Close()
	out := map[string]string{}
	for _, w := range unknown {
		if known[w] {
			continue
		}
		min := 0.45
		if len(w) < 6 {
			min = 0.55
		}
		var fix string
		err := s.Pool.QueryRow(ctx, `SELECT word FROM search_vocab WHERE word % $1 AND similarity(word, $1) >= $2 AND n >= 3
			ORDER BY similarity(word, $1) DESC, n DESC LIMIT 1`, w, min).Scan(&fix)
		if err == nil && fix != "" && fix != w {
			out[w] = fix
		}
	}
	return out
}

// correctQuery respells the words of q that no concept recognised and the catalog does not
// print; it returns q with those words replaced, or "" when nothing changes.
func (s *Store) correctQuery(ctx context.Context, q string, in Intent) string {
	var unknown []string
	for _, c := range in.Concepts {
		if c.Kind == "word" {
			unknown = append(unknown, c.Label)
		}
	}
	fixes := s.SpellFix(ctx, unknown)
	if len(fixes) == 0 {
		return ""
	}
	words := strings.Fields(q)
	changed := false
	for i, w := range words {
		k := strings.ToLower(strings.Trim(w, "-–—.:;()\"“”,"))
		if fix, ok := fixes[k]; ok {
			words[i] = fix
			changed = true
		}
	}
	if !changed {
		return ""
	}
	return strings.Join(words, " ")
}
