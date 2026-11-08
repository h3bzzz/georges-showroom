package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Catalog is a vendor book.
type Catalog struct {
	ID        int
	BrandID   *int
	BrandName *string
	BrandSlug *string
	Code      string
	Name      string
	Title     *string
	Pages     *int
	CoverPath *string
}

// Catalogs lists books, optionally for one brand.
func (s *Store) Catalogs(ctx context.Context, brandSlug string) ([]Catalog, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_id, b.name, b.slug, c.code, c.name, c.title, c.pages, c.cover_path
		FROM catalogs c LEFT JOIN brands b ON b.id=c.brand_id
		WHERE $1 = '' OR b.slug = $1
		ORDER BY b.name NULLS LAST, c.name`, brandSlug)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Catalog])
}

// CatalogByID returns one book.
func (s *Store) CatalogByID(ctx context.Context, id int) (*Catalog, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_id, b.name, b.slug, c.code, c.name, c.title, c.pages, c.cover_path
		FROM catalogs c LEFT JOIN brands b ON b.id=c.brand_id WHERE c.id=$1`, id)
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Catalog])
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// PagePath is the on-disk render of one catalog page ("" when missing).
func (s *Store) PagePath(ctx context.Context, id, page int) (string, error) {
	var p *string
	err := s.Pool.QueryRow(ctx, `SELECT path FROM catalog_pages WHERE catalog_id=$1 AND page=$2`, id, page).Scan(&p)
	if err == pgx.ErrNoRows || p == nil {
		return "", nil
	}
	return *p, err
}

// TOCEntry is a printed or detected section heading.
type TOCEntry struct {
	Page  int
	Title string
	Level int
}

// TOC lists a book's contents, printed index preferred.
func (s *Store) TOC(ctx context.Context, id int) ([]TOCEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT page, title, level FROM catalog_toc WHERE catalog_id=$1
		  AND source = (SELECT CASE WHEN count(*) FILTER (WHERE source='printed') >= 5 THEN 'printed' ELSE min(source) END FROM catalog_toc WHERE catalog_id=$1)
		ORDER BY page, level, title`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TOCEntry])
}

// PageHit is a catalog page matching a text search.
type PageHit struct {
	CatalogID int
	Page      int
	Catalog   string
	Brand     *string
	Snippet   string
}

// SearchPages runs full text over the catalog page text.
func (s *Store) SearchPages(ctx context.Context, q string, limit int) ([]PageHit, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.catalog_id, p.page, COALESCE(c.title, c.name), b.name,
		       ts_headline('simple', left(p.text, 20000), websearch_to_tsquery('simple', $1), 'MaxWords=18, MinWords=8, MaxFragments=1')
		FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id LEFT JOIN brands b ON b.id=c.brand_id
		WHERE p.search @@ websearch_to_tsquery('simple', $1)
		ORDER BY ts_rank(p.search, websearch_to_tsquery('simple', $1)) DESC LIMIT $2`, q, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageHit])
}

// Cover is curated imagery for a room, tile or the home page.
type Cover struct {
	Kind, Key, Path string
	Caption         *string
}

// Covers returns all curated covers keyed by kind|key.
func (s *Store) Covers(ctx context.Context) (map[string]Cover, error) {
	rows, err := s.Pool.Query(ctx, `SELECT kind, key, path, caption FROM covers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Cover{}
	for rows.Next() {
		var c Cover
		if err := rows.Scan(&c.Kind, &c.Key, &c.Path, &c.Caption); err != nil {
			return nil, err
		}
		out[c.Kind+"|"+c.Key] = c
	}
	return out, rows.Err()
}

// TileCard picks a representative trusted card for a category tile.
func (s *Store) TileCards(ctx context.Context, room string, groups []string) (map[string]Card, error) {
	out := map[string]Card{}
	for _, g := range groups {
		rows, err := s.Pool.Query(ctx, `
			SELECT sd.family_id, sd.brand, sd.brand_slug, sd.title, sd.collection, sd.category, sd.price_cents,
			       sd.card_hash, sd.card_w, sd.card_h, sd.card_lqip, sd.grade, sd.tones, sd.finishes, sd.skus[1:4]
			FROM search_docs sd WHERE sd.room=$1 AND sd.groups @> ARRAY[$2]::text[] AND sd.grade = 0 AND sd.tier = 0
			ORDER BY sd.pop DESC, sd.family_id LIMIT 1`, room, g)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c Card
			if err := rows.Scan(&c.FamilyID, &c.Brand, &c.BrandSlug, &c.Title, &c.Collection, &c.Category, &c.PriceCents,
				&c.Hash, &c.W, &c.H, &c.LQIP, &c.Grade, &c.Tones, &c.Finishes, &c.SKUs); err != nil {
				rows.Close()
				return nil, err
			}
			out[g] = c
		}
		rows.Close()
	}
	return out, nil
}
