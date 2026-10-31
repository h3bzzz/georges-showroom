package store

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// DocTask is a spec/install sheet that still has to be downloaded.
type DocTask struct {
	ID        int64
	URL       string
	Kind      string
	FamilyID  *int64
	ProductID *int64
	BrandCode string
}

// DocumentsToFetch lists documents without a local copy. Rows that failed before are skipped
// unless retry is set.
func (s *Store) DocumentsToFetch(ctx context.Context, brandCode string, limit int, retry bool) ([]DocTask, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT d.id, d.url, d.kind, d.family_id, d.product_id, COALESCE(b.code,'')
		FROM documents d
		LEFT JOIN product_families f ON f.id=d.family_id
		LEFT JOIN brands b ON b.id=f.brand_id
		WHERE d.path IS NULL AND ($3 OR d.error IS NULL) AND ($1='' OR b.code=$1)
		ORDER BY d.id LIMIT $2`, brandCode, limit, retry)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DocTask])
}

// DocumentsToImage lists fetched documents for a fresh image extraction (--reimage).
func (s *Store) DocumentsToImage(ctx context.Context, brandCode string, limit int) ([]DocTask, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT d.id, d.url, d.kind, d.family_id, d.product_id, COALESCE(b.code,'')
		FROM documents d
		LEFT JOIN product_families f ON f.id=d.family_id
		LEFT JOIN brands b ON b.id=f.brand_id
		WHERE d.path IS NOT NULL AND ($1='' OR b.code=$1)
		ORDER BY d.id LIMIT $2`, brandCode, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DocTask])
}

// ClearSpecImages forgets the images extracted from one document (files are removed by the caller).
func (s *Store) ClearSpecImages(ctx context.Context, docID int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM product_images WHERE source='spec' AND url LIKE 'spec:'||$1||':%'`, strconv.FormatInt(docID, 10))
	return err
}

func (s *Store) SaveDocument(ctx context.Context, id int64, path, sha1, text string, bytes, pages int) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE documents SET path=$2, sha1=$3, text=NULLIF($4,''), bytes=$5, pages=NULLIF($6,0), fetched_at=now(), error=NULL
		WHERE id=$1`, id, path, sha1, text, bytes, pages)
	return err
}

func (s *Store) SaveDocumentError(ctx context.Context, id int64, msg string) error {
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := s.Pool.Exec(ctx, `UPDATE documents SET error=$2, fetched_at=now() WHERE id=$1`, id, msg)
	return err
}

// AddSpecImage records one image extracted from a spec sheet (source='spec', keyed by
// document id and index so re-runs replace rather than duplicate).
func (s *Store) AddSpecImage(ctx context.Context, docID int64, familyID, productID *int64, n int, path string, w, h int) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO product_images (family_id, product_id, source, url, path, position, width, height)
		VALUES ($1,$2,'spec','spec:'||$3||':'||$4,$5,$6,$7,$8)
		ON CONFLICT (source, url) DO UPDATE SET family_id=EXCLUDED.family_id, product_id=EXCLUDED.product_id,
		  path=EXCLUDED.path, width=EXCLUDED.width, height=EXCLUDED.height`,
		familyID, productID, strconv.FormatInt(docID, 10), strconv.Itoa(n), path, n, w, h)
	return err
}

func (s *Store) MarkDocumentImages(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE documents SET images_at=now() WHERE id=$1`, id)
	return err
}

// DocumentByID returns the stored copy's path (relative to DATA_DIR) and the source URL.
func (s *Store) DocumentByID(ctx context.Context, id int64) (url, path string, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT url, COALESCE(path,'') FROM documents WHERE id=$1`, id).Scan(&url, &path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return url, path, err
}

type DocStats struct {
	Total, Fetched, Failed, WithText, WithImages, SpecImages int
}

func (s *Store) DocumentStats(ctx context.Context) (DocStats, error) {
	var d DocStats
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*)::int, count(path)::int, count(error)::int, count(text)::int, count(images_at)::int,
		  (SELECT count(*)::int FROM product_images WHERE source='spec' AND path IS NOT NULL)
		FROM documents`).Scan(&d.Total, &d.Fetched, &d.Failed, &d.WithText, &d.WithImages, &d.SpecImages)
	return d, err
}
