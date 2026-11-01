package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type TOCEntry struct {
	Page  int
	Title string
	Level int
}

func (s *Store) ReplaceTOC(ctx context.Context, catalogID int, entries []TOCEntry, source string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_toc WHERE catalog_id=$1 AND source=$2`, catalogID, source); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_toc (catalog_id, page, title, level, source) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			catalogID, e.Page, e.Title, e.Level, source); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) TOC(ctx context.Context, catalogID int) ([]TOCEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT page, title, level FROM catalog_toc WHERE catalog_id=$1
		  AND source = (SELECT CASE WHEN EXISTS (SELECT 1 FROM catalog_toc t WHERE t.catalog_id=$1 AND t.source='printed') THEN 'printed' ELSE 'ocr' END)
		ORDER BY page, level`, catalogID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TOCEntry])
}

// PageSKUMap returns, for one catalog, the SKU tokens per page (for the viewer strip).
func (s *Store) PageSKUMap(ctx context.Context, catalogID int) (map[int][]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT page, sku FROM catalog_skus WHERE catalog_id=$1 ORDER BY page, x`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]string{}
	for rows.Next() {
		var p int
		var k string
		if err := rows.Scan(&p, &k); err != nil {
			return nil, err
		}
		if len(out[p]) < 24 {
			out[p] = append(out[p], k)
		}
	}
	return out, rows.Err()
}

// BrandCollections returns a brand's display name and the collection names its families
// carry (two families or more), the anchors toc.Fold trusts over OCR headings.
func (s *Store) BrandCollections(ctx context.Context, code string) (string, []string, error) {
	name := code
	_ = s.Pool.QueryRow(ctx, `SELECT name FROM brands WHERE code=$1`, code).Scan(&name)
	rows, err := s.Pool.Query(ctx, `
		SELECT f.collection FROM product_families f JOIN brands b ON b.id=f.brand_id
		WHERE b.code=$1 AND f.collection IS NOT NULL AND f.collection <> ''
		GROUP BY f.collection HAVING count(*) >= 2 ORDER BY f.collection`, code)
	if err != nil {
		return name, nil, err
	}
	cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	return name, cols, err
}

// CatalogProgress reports how many of a book's pages have been OCR'd.
func (s *Store) CatalogProgress(ctx context.Context, catalogID int) int {
	n := 0
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM catalog_pages WHERE catalog_id=$1 AND ocr_at IS NOT NULL`, catalogID).Scan(&n)
	return n
}

// SKUPages finds catalog pages that print a SKU (exact, by base model, as a prefix, or by
// the model before the dash when the OCR never read the full token), for the search box
// when no product carries it yet. The snippet lists the SKUs seen.
func (s *Store) SKUPages(ctx context.Context, sku string, limit int) ([]CatalogHit, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, COALESCE(c.title,c.name), k.page, string_agg(DISTINCT k.sku, ' ')
		FROM catalog_skus k JOIN catalogs c ON c.id=k.catalog_id
		WHERE k.sku = $1 OR k.sku_base = $1 OR k.sku LIKE $1 || '%'
		   OR (length(split_part($1,'-',1)) >= 4 AND k.sku LIKE split_part($1,'-',1) || '-%')
		GROUP BY c.id, c.brand_code, 3, k.page
		ORDER BY bool_or(k.sku = $1) DESC, bool_or(k.sku LIKE $1 || '%') DESC, c.brand_code, k.page LIMIT $2`, sku, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogHit])
}

// CatalogCard is a book on the catalogs index with its indexing state.
type CatalogCard struct {
	CatalogRow
	Sections int // contents entries
	OCRd     int // pages read
}

// CatalogGroup is one brand's shelf.
type CatalogGroup struct {
	Code, Slug, Name, Logo string
	Books                  []CatalogCard
}

// CatalogGroups lists every book grouped by brand, brands in name order.
func (s *Store) CatalogGroups(ctx context.Context) ([]CatalogGroup, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, c.name, COALESCE(c.title,''), COALESCE(c.pages,0), COALESCE(c.has_text,false), COALESCE(c.fetched_at,'epoch'::timestamptz),
		  COALESCE(b.name, c.brand_code), COALESCE(b.slug,''), COALESCE(b.logo,''),
		  (SELECT count(*) FROM catalog_toc t WHERE t.catalog_id=c.id
		     AND t.source = CASE WHEN EXISTS (SELECT 1 FROM catalog_toc x WHERE x.catalog_id=c.id AND x.source='printed') THEN 'printed' ELSE 'ocr' END),
		  (SELECT count(*) FROM catalog_pages p WHERE p.catalog_id=c.id AND p.ocr_at IS NOT NULL)
		FROM catalogs c LEFT JOIN brands b ON b.code=c.brand_code
		ORDER BY COALESCE(b.name, c.brand_code), c.brand_code, c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogGroup
	for rows.Next() {
		var card CatalogCard
		var name, slug, logo string
		if err := rows.Scan(&card.ID, &card.BrandCode, &card.Name, &card.Title, &card.Pages, &card.HasText, &card.FetchedAt,
			&name, &slug, &logo, &card.Sections, &card.OCRd); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].Code != card.BrandCode {
			out = append(out, CatalogGroup{Code: card.BrandCode, Slug: slug, Name: name, Logo: logo})
		}
		out[len(out)-1].Books = append(out[len(out)-1].Books, card)
	}
	return out, rows.Err()
}
