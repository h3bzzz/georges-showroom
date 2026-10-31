package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

type PageRef struct {
	CatalogID int
	BrandCode string
	Name      string
	Page      int
	Pages     int
}

// PagesNeedingOCR lists catalog pages (for a brand code or all) not yet OCR'd.
func (s *Store) PagesNeedingOCR(ctx context.Context, brand string, limit int) ([]PageRef, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, c.name, g.page, c.pages
		FROM catalogs c CROSS JOIN LATERAL generate_series(1, COALESCE(c.pages,0)) g(page)
		LEFT JOIN catalog_pages p ON p.catalog_id=c.id AND p.page=g.page
		WHERE ($1='' OR c.brand_code=$1) AND (p.ocr_at IS NULL)
		ORDER BY c.brand_code, c.name, g.page LIMIT $2`, brand, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageRef])
}

// PagesNeedingExtract lists OCR'd pages not yet cropped.
func (s *Store) PagesNeedingExtract(ctx context.Context, brand string, limit int) ([]PageRef, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, c.name, p.page, c.pages
		FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id
		WHERE ($1='' OR c.brand_code=$1) AND p.ocr_at IS NOT NULL AND p.extracted_at IS NULL
		ORDER BY c.brand_code, c.name, p.page LIMIT $2`, brand, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageRef])
}

type SKUBox struct {
	SKU        string
	X, Y, W, H int
	Conf       float64
}

func (s *Store) SaveOCR(ctx context.Context, catalogID, page int, path, text string, skus []SKUBox) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO catalog_pages (catalog_id, page, path, ocr_text, ocr_at) VALUES ($1,$2,$3,$4,now())
		ON CONFLICT (catalog_id, page) DO UPDATE SET path=COALESCE(catalog_pages.path, EXCLUDED.path), ocr_text=EXCLUDED.ocr_text, ocr_at=now()`,
		catalogID, page, path, text); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_skus WHERE catalog_id=$1 AND page=$2`, catalogID, page); err != nil {
		return err
	}
	for _, k := range skus {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_skus (catalog_id, page, sku, x, y, w, h, conf, sku_base) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')) ON CONFLICT DO NOTHING`,
			catalogID, page, k.SKU, k.X, k.Y, k.W, k.H, k.Conf, SKUBase(k.SKU)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) PageSKUs(ctx context.Context, catalogID, page int) ([]SKUBox, error) {
	rows, err := s.Pool.Query(ctx, `SELECT sku, x, y, w, h, conf FROM catalog_skus WHERE catalog_id=$1 AND page=$2`, catalogID, page)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SKUBox])
}

type CropRow struct {
	ID         int64
	CatalogID  int
	Page       int
	X, Y, W, H int
	Path       string
	SKU        string
	Score      float64
	Kind       string
}

func (s *Store) SaveCrops(ctx context.Context, catalogID, page int, crops []CropRow) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_crops WHERE catalog_id=$1 AND page=$2`, catalogID, page); err != nil {
		return err
	}
	for _, c := range crops {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_crops (catalog_id, page, x, y, w, h, path, sku, score, kind, sku_base) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,NULLIF($11,'')) ON CONFLICT DO NOTHING`,
			catalogID, page, c.X, c.Y, c.W, c.H, c.Path, c.SKU, c.Score, c.Kind, SKUBase(c.SKU)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE catalog_pages SET extracted_at=now() WHERE catalog_id=$1 AND page=$2`, catalogID, page); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CropsForSKU returns catalog crops whose OCR'd SKU matches (exact, then prefix).
func (s *Store) CropsForSKU(ctx context.Context, sku string) ([]CropRow, error) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	rows, err := s.Pool.Query(ctx, `
		SELECT id, catalog_id, page, x, y, w, h, path, COALESCE(sku,''), COALESCE(score,0), COALESCE(kind,'photo')
		FROM catalog_crops WHERE sku=$1 OR $1 LIKE sku||'%' OR sku LIKE $1||'%'
		ORDER BY (sku=$1) DESC, (kind='photo') DESC, score DESC, w*h DESC LIMIT 12`, sku)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CropRow])
}

// CropsForPage returns crops on one page.
func (s *Store) CropsForPage(ctx context.Context, catalogID, page int) ([]CropRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, catalog_id, page, x, y, w, h, path, COALESCE(sku,''), COALESCE(score,0), COALESCE(kind,'photo') FROM catalog_crops WHERE catalog_id=$1 AND page=$2 ORDER BY y, x`, catalogID, page)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CropRow])
}

type ExtractStats struct{ OCRPages, Skus, Crops, LinkedCrops int }

func (s *Store) ExtractCounts(ctx context.Context) (ExtractStats, error) {
	var e ExtractStats
	err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM catalog_pages WHERE ocr_at IS NOT NULL),(SELECT count(*) FROM catalog_skus),(SELECT count(*) FROM catalog_crops),(SELECT count(*) FROM catalog_crops WHERE sku IS NOT NULL)`).
		Scan(&e.OCRPages, &e.Skus, &e.Crops, &e.LinkedCrops)
	return e, err
}

// ReplacePageSKUs rewrites the SKU boxes for one page.
func (s *Store) ReplacePageSKUs(ctx context.Context, catalogID, page int, skus []SKUBox) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_skus WHERE catalog_id=$1 AND page=$2`, catalogID, page); err != nil {
		return err
	}
	for _, k := range skus {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_skus (catalog_id, page, sku, x, y, w, h, conf, sku_base) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')) ON CONFLICT DO NOTHING`,
			catalogID, page, k.SKU, k.X, k.Y, k.W, k.H, k.Conf, SKUBase(k.SKU)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ResetExtract clears extraction for a brand (or all) so `extract --force` reprocesses.
func (s *Store) ResetExtract(ctx context.Context, brand string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE catalog_pages p SET extracted_at=NULL FROM catalogs c WHERE c.id=p.catalog_id AND ($1='' OR c.brand_code=$1)`, brand)
	return err
}

// OCRPages lists every OCR'd page (optionally for one brand).
func (s *Store) OCRPages(ctx context.Context, brand string) ([]PageRef, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, c.name, p.page, c.pages
		FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id
		WHERE ($1='' OR c.brand_code=$1) AND p.ocr_at IS NOT NULL
		ORDER BY c.brand_code, c.name, p.page`, brand)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PageRef])
}
