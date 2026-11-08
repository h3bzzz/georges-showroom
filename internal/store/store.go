// Package store is the read model the storefront uses: a handful of indexed queries over
// search_docs, families, items, family_media and catalogs. No ranking logic lives here that
// could run offline instead.
package store

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool

	mu     sync.Mutex
	brands []Brand
	brandT time.Time
}

func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// Brand is a manufacturer.
type Brand struct {
	ID       int
	Code     *string
	Slug     string
	Name     string
	Room     *string
	Website  *string
	LogoPath *string
	Featured bool
	Families int
}

// Brands lists brands with family counts, cached for five minutes.
func (s *Store) Brands(ctx context.Context) ([]Brand, error) {
	s.mu.Lock()
	if time.Since(s.brandT) < 5*time.Minute && s.brands != nil {
		b := s.brands
		s.mu.Unlock()
		return b, nil
	}
	s.mu.Unlock()
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, b.code, b.slug, b.name, b.room, b.website, b.logo_path, b.featured, COALESCE(n.c, 0)
		FROM brands b
		LEFT JOIN (SELECT brand_id, count(*)::int AS c FROM search_docs WHERE tier <> 2 GROUP BY brand_id) n ON n.brand_id = b.id
		ORDER BY b.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Brand
	for rows.Next() {
		var b Brand
		if err := rows.Scan(&b.ID, &b.Code, &b.Slug, &b.Name, &b.Room, &b.Website, &b.LogoPath, &b.Featured, &b.Families); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	s.mu.Lock()
	s.brands, s.brandT = out, time.Now()
	s.mu.Unlock()
	return out, rows.Err()
}

// BrandBySlug returns one brand (nil when unknown).
func (s *Store) BrandBySlug(ctx context.Context, slug string) (*Brand, error) {
	bs, err := s.Brands(ctx)
	if err != nil {
		return nil, err
	}
	for i := range bs {
		if bs[i].Slug == slug {
			return &bs[i], nil
		}
	}
	return nil, nil
}
