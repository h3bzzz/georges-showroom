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
