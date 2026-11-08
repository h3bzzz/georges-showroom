// Package search builds and queries search_docs: one row per family carrying everything
// browse, search and typeahead need, with a weighted tsvector and a trigram blob. The web
// layer runs one indexed query per page and joins nothing.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"georges/internal/taxonomy"
)

// Indexer rebuilds search_docs.
type Indexer struct {
	Pool *pgxpool.Pool
}

type docRow struct {
	FamilyID   int64
	BrandID    int
	Brand      string
	BrandSlug  string
	Title      string
	Collection *string
	Room       *string
	Category   *string
	Tier       int16
	Finishes   []string
	SKUs       []string
	PriceCents *int64
	CardHash   *string
	CardW      *int
	CardH      *int
	CardLQIP   *int
	Grade      *int16
	Facts      string
}

// Run rebuilds the whole index in one transaction (≈45k rows, a few seconds).
func (ix *Indexer) Run(ctx context.Context) (int, error) {
	start := time.Now()
	rows, err := ix.Pool.Query(ctx, `
		SELECT f.id, b.id, b.name, b.slug, COALESCE(f.display_title, f.title, ''), f.collection, f.room, f.category, f.tier,
		       f.finishes,
		       COALESCE((SELECT array_agg(i.sku ORDER BY i.id) FROM items i WHERE i.family_id=f.id), '{}'),
		       (SELECT min(i.price_cents) FROM items i WHERE i.family_id=f.id AND i.price_cents IS NOT NULL AND i.price_flag IS NULL),
		       a.hash, a.width, a.height, a.lqip, fm.grade,
		       COALESCE((SELECT string_agg(v->>'value', ' ') FROM jsonb_each(f.facts) kv(k, v) WHERE k IN ('Collection','Mounting','Material','Handle Style','Function','Designer')), '')
		FROM families f
		JOIN brands b ON b.id=f.brand_id
		LEFT JOIN family_media fm ON fm.family_id=f.id AND fm.role='card' AND fm.rank=0
		LEFT JOIN assets a ON a.id=fm.asset_id
		WHERE f.tier <> 3 OR EXISTS (SELECT 1 FROM items i WHERE i.family_id=f.id)`)
	if err != nil {
		return 0, err
	}
	docs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[docRow])
	if err != nil {
		return 0, err
	}
	tx, err := ix.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `TRUNCATE search_docs`); err != nil {
		return 0, err
	}
	groupsOf := map[string][]string{}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"search_docs"},
		[]string{"family_id", "brand_id", "brand", "brand_slug", "title", "collection", "room", "category", "groups", "tier", "grade", "tones", "finishes", "price_cents", "card_hash", "card_w", "card_h", "card_lqip", "skus", "blob", "tsv", "pop"},
		pgx.CopyFromSlice(len(docs), func(i int) ([]any, error) {
			d := docs[i]
			cat := deref(d.Category)
			groups, ok := groupsOf[cat]
			if !ok {
				groups = taxonomy.GroupsFor(cat)
				if groups == nil {
					groups = []string{}
				}
				groupsOf[cat] = groups
			}
			grade := int16(3)
			if d.Grade != nil {
				grade = *d.Grade
			}
			tones := tonesOf(d.Finishes)
			skus := d.SKUs
			if skus == nil {
				skus = []string{}
			}
			blob := strings.ToLower(strings.Join(compact(d.Title, deref(d.Collection), d.Brand, strings.Join(skus, " "), strings.Join(d.Finishes, " ")), " "))
			// tsv is filled by the UPDATE below; COPY cannot call to_tsvector per row
			// pop: what makes a card worth showing first within its bucket — a priced product
			// with a title that reads cleanly, several finishes, a collection, and a big picture
			pop := 0.0
			if d.PriceCents != nil {
				pop += 1
			}
			if d.Collection != nil && *d.Collection != "" {
				pop += 0.5
			}
			pop += math.Min(1, float64(len(d.Finishes))/6)
			if d.CardW != nil && *d.CardW >= 600 {
				pop += 0.5
			}
			if strings.Contains(strings.ToLower(d.Title), "continued") || len(d.Title) > 70 || strings.Count(d.Title, " ") < 1 {
				pop -= 1
			}
			return []any{d.FamilyID, d.BrandID, d.Brand, d.BrandSlug, d.Title, d.Collection, d.Room, d.Category, groups, d.Tier, grade,
				tones, d.Finishes, d.PriceCents, d.CardHash, d.CardW, d.CardH, d.CardLQIP, skus, blob, "", pop}, nil
		}))
	if err != nil {
		return 0, fmt.Errorf("copy: %w", err)
	}
	// weighted text: title and SKUs matter most, then collection and brand, then category,
	// then the facts a shopper might type (mounting, material, designer)
	_, err = tx.Exec(ctx, `
		UPDATE search_docs sd SET tsv =
		    setweight(to_tsvector('english', sd.title), 'A')
		 || setweight(to_tsvector('simple', array_to_string(sd.skus, ' ')), 'A')
		 || setweight(to_tsvector('simple', coalesce(sd.collection,'') || ' ' || sd.brand), 'B')
		 || setweight(to_tsvector('english', coalesce(sd.category,'') || ' ' || array_to_string(sd.groups, ' ')), 'C')
		 || setweight(to_tsvector('english', array_to_string(sd.finishes, ' ') || ' ' || coalesce(x.facts,'')), 'D')
		FROM (SELECT f.id, COALESCE((SELECT string_agg(v->>'value', ' ') FROM jsonb_each(f.facts) kv(k, v) WHERE k IN ('Collection','Mounting','Material','Handle Style','Function','Designer')), '') AS facts FROM families f) x
		WHERE x.id = sd.family_id`)
	if err != nil {
		return 0, fmt.Errorf("tsv: %w", err)
	}
	// brand round-robin ranks (see migration 00004): two cards per brand per round
	_, err = tx.Exec(ctx, `
		UPDATE search_docs sd SET rr_room = x.rr_room, rr_cat = x.rr_cat FROM (
		  SELECT family_id,
		    (row_number() OVER (PARTITION BY brand_id, room, tier, LEAST(grade, 2) ORDER BY pop DESC, family_id) - 1) / 2 AS rr_room,
		    (row_number() OVER (PARTITION BY brand_id, room, category, tier, LEAST(grade, 2) ORDER BY pop DESC, family_id) - 1) / 2 AS rr_cat
		  FROM search_docs) x WHERE x.family_id = sd.family_id`)
	if err != nil {
		return 0, fmt.Errorf("round robin: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	ix.Pool.Exec(ctx, `ANALYZE search_docs`)
	slog.Info("index built", "docs", n, "took", time.Since(start).Round(time.Millisecond))
	return int(n), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func compact(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// tonesOf folds finish names into the broad tone groups cards and filters use.
func tonesOf(finishes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range finishes {
		e := taxonomy.ClassifyFinish(f)
		for _, t := range e.Tones {
			g := taxonomy.ToneGroup(t)
			if g != "other" && !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	sort.Strings(out)
	if out == nil {
		return []string{}
	}
	return out
}
