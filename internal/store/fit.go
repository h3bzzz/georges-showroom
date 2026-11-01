package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// FitCandidate is one crop that could stand as a family's card photo: a crop linked to the
// family by SKU or page, or a series opener (a photograph on one of the four pages above the
// family's price-book line, the fallback RefreshFamilyBest uses when nothing is linked).
type FitCandidate struct {
	FamilyID int64
	CropID   int64
	Emb      []float32
	Source   string // crop | opener
}

// FitFamily is a family with the text the picture should show and its candidate crops.
type FitFamily struct {
	ID         int64
	Title      string
	Category   string
	Candidates []FitCandidate
}

var reFitNoise = regexp.MustCompile(`(?i)\(continued\)|\bmodel no\.?\b|\bno\.\s*\S+|[#"]|\d+\s*/\s*\d+|\b\d{3,}[A-Z0-9-]*\b|\s{2,}`)

// FitText is the sentence whose CLIP embedding a candidate picture is compared with: the
// family's title without part numbers, plus its merchandised category when one is known
// ("a photo of a Toilet Brush Set, a bath accessory").
func FitText(title, category string) string {
	t := strings.TrimSpace(reFitNoise.ReplaceAllString(title, " "))
	t = strings.Join(strings.Fields(t), " ")
	if len(t) > 70 {
		t = t[:70]
	}
	if t == "" && category == "" {
		return ""
	}
	s := "a photo of a " + t
	if t == "" {
		s = "a photo of a"
	}
	if category != "" {
		c := strings.ToLower(category)
		c = strings.TrimSuffix(strings.TrimSuffix(c, "s"), "ie")
		if strings.HasSuffix(strings.ToLower(category), "ies") {
			c += "y"
		}
		s += ", a " + c
	}
	return s
}

// FitCandidates streams every family (optionally one brand) with its candidate crops and their
// stored embeddings. Families with no candidate are skipped.
func (s *Store) FitCandidates(ctx context.Context, brand, model string, fn func(FitFamily) error) error {
	rows, err := s.Pool.Query(ctx, `
		WITH placed AS (
		  SELECT fb.family_id, fb.page_catalog AS catalog_id, fb.page_no AS page
		  FROM family_best fb WHERE fb.page_catalog IS NOT NULL
		  UNION
		  (SELECT DISTINCT ON (p.family_id) p.family_id, k.catalog_id, k.page
		   FROM family_best fb
		   JOIN products p ON p.family_id=fb.family_id
		   JOIN brands b ON b.id=p.brand_id
		   JOIN catalogs c ON c.brand_code=b.code
		   JOIN catalog_skus k ON k.catalog_id=c.id AND k.sku=p.sku
		   WHERE fb.page_catalog IS NULL
		   ORDER BY p.family_id, k.catalog_id, k.page)
		), openers AS (
		  SELECT cc.catalog_id, cc.page, cc.id
		  FROM catalog_crops cc
		  WHERE (cc.kind='photo' OR (cc.vkind='photo' AND COALESCE(cc.vscore,0) >= 0.6 AND COALESCE(cc.quality,0) >= 0.2)) AND cc.w >= 300 AND cc.h >= 200
		    AND (cc.vkind IS NULL OR cc.vkind = 'photo') AND cc.vemb IS NOT NULL
		    AND (SELECT count(*) FROM catalog_crops o WHERE o.catalog_id=cc.catalog_id AND o.page=cc.page AND o.kind='photo') <= 2
		), cands AS (
		  SELECT fc.family_id, fc.crop_id, 'crop' AS src FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id
		  WHERE cc.vemb IS NOT NULL AND (cc.kind='photo' OR cc.vkind='photo')
		  UNION
		  SELECT pl.family_id, o.id, 'opener' FROM placed pl JOIN openers o ON o.catalog_id=pl.catalog_id AND o.page BETWEEN pl.page-4 AND pl.page
		  WHERE NOT EXISTS (SELECT 1 FROM catalog_toc t WHERE t.catalog_id=o.catalog_id AND t.source='printed' AND t.page > o.page AND t.page <= pl.page)
		)
		SELECT f.id, COALESCE(f.title,''), COALESCE(f.category_path[1],''), cd.crop_id, cc.vemb, cd.src
		FROM cands cd
		JOIN product_families f ON f.id=cd.family_id
		JOIN brands b ON b.id=f.brand_id
		JOIN catalog_crops cc ON cc.id=cd.crop_id AND cc.vmodel=$2
		WHERE ($1='' OR b.code=$1)
		ORDER BY f.id, cd.src, cd.crop_id`, brand, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cur FitFamily
	flush := func() error {
		if cur.ID == 0 || len(cur.Candidates) == 0 {
			return nil
		}
		return fn(cur)
	}
	for rows.Next() {
		var id, cropID int64
		var title, cat, src string
		var raw []byte
		if err := rows.Scan(&id, &title, &cat, &cropID, &raw, &src); err != nil {
			return err
		}
		if id != cur.ID {
			if err := flush(); err != nil {
				return err
			}
			cur = FitFamily{ID: id, Title: title, Category: cat}
		}
		cur.Candidates = append(cur.Candidates, FitCandidate{FamilyID: id, CropID: cropID, Emb: DecodeEmbedding(raw), Source: src})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

// CropFit is one (family, crop) fit score.
type CropFit struct {
	FamilyID int64
	CropID   int64
	Fit      float32
}

// SaveCropFits upserts fit scores.
func (s *Store) SaveCropFits(ctx context.Context, rows []CropFit) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`INSERT INTO crop_fit (family_id, crop_id, fit) VALUES ($1,$2,$3) ON CONFLICT (family_id, crop_id) DO UPDATE SET fit=EXCLUDED.fit`, r.FamilyID, r.CropID, r.Fit)
	}
	return s.Pool.SendBatch(ctx, b).Close()
}

// CropScene is one crop's scene probability.
type CropScene struct {
	ID    int64
	Scene float32
}

// SaveCropScenes stores scene probabilities.
func (s *Store) SaveCropScenes(ctx context.Context, rows []CropScene) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`UPDATE catalog_crops SET vscene=$2 WHERE id=$1`, r.ID, r.Scene)
	}
	return s.Pool.SendBatch(ctx, b).Close()
}

// RefreshPhotoFit copies the fit of the crop each card shows onto family_best.
func (s *Store) RefreshPhotoFit(ctx context.Context) (int64, error) {
	t, err := s.Pool.Exec(ctx, `
		UPDATE family_best fb SET photo_fit = sub.fit
		FROM (SELECT fb.family_id, cf.fit FROM family_best fb
		      JOIN catalog_crops cc ON fb.photo_path IS NOT NULL AND (cc.path=fb.photo_path OR cc.hd_path=fb.photo_path)
		      JOIN crop_fit cf ON cf.family_id=fb.family_id AND cf.crop_id=cc.id) sub
		WHERE fb.family_id=sub.family_id AND fb.photo_fit IS DISTINCT FROM sub.fit`)
	if err != nil {
		return 0, err
	}
	return t.RowsAffected(), nil
}
