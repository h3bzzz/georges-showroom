package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// HDCandidate is a small crop that would benefit from super-resolution.
type HDCandidate struct {
	ID   int64
	Path string
	W, H int
	// where the crop came from, so the upscaler can re-cut it losslessly from the page render
	BrandCode, Name string
	Page, X, Y      int
}

// CropsNeedingHD lists crops below maxW pixels wide that have not been upscaled yet. Unless all
// is set only crops currently shown as a family's photo or drawing qualify; a family's photo
// comes before its drawing; premier (tier 0) families first, then brand code, book, page.
func (s *Store) CropsNeedingHD(ctx context.Context, brandCode string, maxW, limit int, all bool) ([]HDCandidate, error) {
	rows, err := s.Pool.Query(ctx, `
		WITH shown AS (
		  SELECT path, min(tier) AS tier, bool_or(photo) AS photo FROM (
		    SELECT fb.photo_path AS path, f.tier, true AS photo
		    FROM family_best fb JOIN product_families f ON f.id=fb.family_id WHERE fb.photo_path IS NOT NULL
		    UNION ALL
		    SELECT fb.drawing_path, 9, false FROM family_best fb WHERE fb.drawing_path IS NOT NULL
		  ) x GROUP BY path)
		SELECT cc.id, cc.path, cc.w, cc.h, c.brand_code, c.name, cc.page, cc.x, cc.y
		FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		LEFT JOIN shown s ON s.path=cc.path
		WHERE cc.hd_w IS NULL AND cc.dup_of IS NULL AND cc.w < $1 AND cc.w >= 40 AND cc.h >= 40
		  AND ($2 = '' OR c.brand_code = $2)
		  AND ($3 OR s.path IS NOT NULL)
		ORDER BY (COALESCE(s.tier, 9) = 0) DESC, (s.photo IS TRUE) DESC,
		         (cc.kind='photo') DESC, c.brand_code, cc.catalog_id, cc.page, cc.id
		LIMIT $4`, maxW, brandCode, all, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[HDCandidate])
}

// HDResult records one upscale; an empty Path with W=-1 marks a failure so it is not retried.
type HDResult struct {
	ID   int64
	Path string
	W    int
}

// SetCropHD stores the upscaled paths and swaps them into family_best so pages pick them up
// immediately. Returns how many family_best rows were promoted.
func (s *Store) SetCropHD(ctx context.Context, res []HDResult) (int64, error) {
	b := &pgx.Batch{}
	for _, r := range res {
		b.Queue(`UPDATE catalog_crops SET hd_path=NULLIF($2,''), hd_w=$3 WHERE id=$1`, r.ID, r.Path, r.W)
	}
	if err := s.Pool.SendBatch(ctx, b).Close(); err != nil {
		return 0, err
	}
	return s.PromoteHD(ctx)
}

// PromoteHD points family_best at the HD copy wherever one exists.
func (s *Store) PromoteHD(ctx context.Context) (int64, error) {
	t1, err := s.Pool.Exec(ctx, `UPDATE family_best fb SET photo_path=cc.hd_path, photo_w=cc.hd_w
		FROM catalog_crops cc WHERE fb.photo_path=cc.path AND cc.hd_path IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	t2, err := s.Pool.Exec(ctx, `UPDATE family_best fb SET drawing_path=cc.hd_path
		FROM catalog_crops cc WHERE fb.drawing_path=cc.path AND cc.hd_path IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	return t1.RowsAffected() + t2.RowsAffected(), nil
}

// HDStats reports progress for the upscale job.
func (s *Store) HDStats(ctx context.Context) (done, failed, shownSmall int, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM catalog_crops WHERE hd_path IS NOT NULL),
		(SELECT count(*) FROM catalog_crops WHERE hd_w = -1),
		(SELECT count(*) FROM family_best fb JOIN catalog_crops cc ON cc.path=fb.photo_path WHERE cc.w < 500 AND cc.hd_w IS NULL)`).
		Scan(&done, &failed, &shownSmall)
	return
}
