package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// CoverKey builds the key for a category cover.
func CoverKey(room, category string) string { return room + "|" + category }

func (s *Store) SetCover(ctx context.Context, kind, key, path string, catalogID, page int) error {
	defer InvalidateHomeTiles()
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO covers (kind, key, path, catalog_id, page) VALUES ($1,$2,$3,NULLIF($4,0),NULLIF($5,0))
		ON CONFLICT (kind, key) DO UPDATE SET path=EXCLUDED.path, catalog_id=EXCLUDED.catalog_id, page=EXCLUDED.page, updated_at=now()`,
		kind, key, path, catalogID, page)
	return err
}

func (s *Store) ClearCover(ctx context.Context, kind, key string) error {
	defer InvalidateHomeTiles()
	_, err := s.Pool.Exec(ctx, `DELETE FROM covers WHERE kind=$1 AND key=$2`, kind, key)
	return err
}

// GetCover returns a curated cover or an empty Cover.
func (s *Store) GetCover(ctx context.Context, kind, key string) (Cover, error) {
	var c Cover
	err := s.Pool.QueryRow(ctx, `SELECT path, COALESCE(catalog_id,0), COALESCE(page,0) FROM covers WHERE kind=$1 AND key=$2`, kind, key).Scan(&c.Path, &c.CatalogID, &c.Page)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cover{}, nil
	}
	return c, err
}

// CoverCandidates lists wide lifestyle photos for a room ("" = any room), optionally filtered by
// a category keyword, largest first, so a person can pick the right one.
func (s *Store) CoverCandidates(ctx context.Context, room, category string, limit int) ([]Cover, error) {
	return s.coverCandidates(ctx, room, category, limit, 600, 350)
}

// coverCandidates is CoverCandidates with the minimum photo size as parameters.
func (s *Store) coverCandidates(ctx context.Context, room, category string, limit, minW, minH int) ([]Cover, error) {
	kw := ""
	if category != "" {
		kw = keywordFor(category)
	}
	// relevance: a catalog whose title is about the category (a bathtub price book) beats a
	// page that merely mentions the word; then how often the word appears; then size
	rows, err := s.Pool.Query(ctx, `
		SELECT path, catalog_id, page, w, h FROM (
		  SELECT DISTINCT ON (cc.catalog_id, cc.page) cc.path, cc.catalog_id, cc.page, cc.w, cc.h,
		    (CASE WHEN $2 <> '' AND COALESCE(cat.title,'') ILIKE '%'||$2||'%' THEN 3 ELSE 0 END)
		    + LEAST(5, (length(COALESCE(p.ocr_text,'')) - length(replace(lower(COALESCE(p.ocr_text,'')), lower($2), ''))) / GREATEST(length($2),1)) AS rel
		  FROM catalog_crops cc
		  JOIN catalog_pages p ON p.catalog_id=cc.catalog_id AND p.page=cc.page
		  JOIN catalogs cat ON cat.id=cc.catalog_id
		  JOIN brands b ON b.code=cat.brand_code
		  WHERE cc.kind='photo' AND cc.w >= $4 AND cc.h >= $5 AND length(COALESCE(p.ocr_text,'')) < 900
		    AND ($1 = '' OR brand_room(b.id) = $1)
		    AND ($2 = '' OR COALESCE(p.ocr_text,'') ILIKE '%'||$2||'%' OR COALESCE(p.text,'') ILIKE '%'||$2||'%' OR COALESCE(cat.title,'') ILIKE '%'||$2||'%')
		  ORDER BY cc.catalog_id, cc.page, cc.w*cc.h DESC
		) x ORDER BY rel DESC, w*h DESC LIMIT $3`, room, kw, limit, minW, minH)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Cover])
}

// CoverFor resolves the image for a room banner or category tile: curated pick, else automatic.
func (s *Store) CoverFor(ctx context.Context, kind, key string, auto func() Cover) Cover {
	if c, _ := s.GetCover(ctx, kind, key); c.Path != "" {
		return c
	}
	return auto()
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Slugify is exported for file-override lookups (DATA_DIR/covers/<room>/<category>.jpg).
func Slugify(s string) string { return slugify(s) }
