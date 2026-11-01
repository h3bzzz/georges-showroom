package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ToneExample is a crop whose finish is known: it is linked to a SKU that carries one.
type ToneExample struct {
	ID    int64
	Emb   []float32
	Group string // vision tone class (ToneGroup of the SKU's finish)
}

// ToneExamples returns vendor images (product_images with an embedding) labelled with the tone
// group of their SKU's finish. Vendor masters are keyed by SKU, so they show that finish;
// price-book crops do not (one photo, usually chrome, stands for every finish of a model),
// which is why they are not used as examples; spec-sheet images (source='spec') are drawings.
func (s *Store) ToneExamples(ctx context.Context, model string, limitPerGroup int) ([]ToneExample, error) {
	dict := s.finishDict(ctx)
	rows, err := s.Pool.Query(ctx, `
		SELECT pi.id, pi.vemb, p.finish
		FROM product_images pi JOIN products p ON p.id=pi.product_id
		WHERE pi.vemb IS NOT NULL AND pi.vmodel=$1 AND pi.source='web' AND COALESCE(p.finish,'')<>'' AND COALESCE(pi.vkind,'photo')='photo'`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	per := map[string]int{}
	var out []ToneExample
	for rows.Next() {
		var id int64
		var raw []byte
		var finish string
		if err := rows.Scan(&id, &raw, &finish); err != nil {
			return nil, err
		}
		e := dict.Entry(finish)
		if e == nil || e.Head == "" {
			continue
		}
		g := ToneGroup(e.Head)
		if g == "other" || (limitPerGroup > 0 && per[g] >= limitPerGroup) {
			continue
		}
		per[g]++
		out = append(out, ToneExample{ID: id, Emb: DecodeEmbedding(raw), Group: g})
	}
	return out, rows.Err()
}

// PhotoEmbeddings streams the stored embeddings of photo crops (drawings and text carry no
// tone).
func (s *Store) PhotoEmbeddings(ctx context.Context, brand, model string, fn func(CropEmbedding) error) error {
	rows, err := s.Pool.Query(ctx, `SELECT cc.id, cc.path, cc.vemb FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		WHERE cc.vemb IS NOT NULL AND cc.vmodel=$2 AND cc.kind='photo' AND ($1='' OR c.brand_code=$1) ORDER BY cc.id`, brand, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e CropEmbedding
		var raw []byte
		if err := rows.Scan(&e.ID, &e.Path, &raw); err != nil {
			return err
		}
		e.Emb = DecodeEmbedding(raw)
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// CropTone is one crop's tone verdict.
type CropTone struct {
	ID    int64
	Tone  string
	Score float32
}

// SaveCropTones stores tone verdicts.
func (s *Store) SaveCropTones(ctx context.Context, rows []CropTone) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		b.Queue(`UPDATE catalog_crops SET vtone=$2, vtonescore=$3 WHERE id=$1`, r.ID, r.Tone, r.Score)
	}
	return s.Pool.SendBatch(ctx, b).Close()
}

// RefreshImageTones sets product_images.vtone from the finish of the SKU each vendor image
// belongs to (no vision needed: vendor masters are keyed by SKU). Returns rows set.
func (s *Store) RefreshImageTones(ctx context.Context) (int64, error) {
	dict := s.finishDict(ctx)
	b := &pgx.Batch{}
	n := 0
	for _, e := range dict.Entries {
		if e.Head == "" {
			continue
		}
		b.Queue(`UPDATE product_images pi SET vtone=$2 FROM products p WHERE p.id=pi.product_id AND p.finish=$1 AND pi.vtone IS DISTINCT FROM $2`, e.Name, ToneGroup(e.Head))
		n++
	}
	if n == 0 {
		return 0, nil
	}
	res := s.Pool.SendBatch(ctx, b)
	defer res.Close()
	var total int64
	for i := 0; i < n; i++ {
		t, err := res.Exec()
		if err != nil {
			return total, err
		}
		total += t.RowsAffected()
	}
	return total, nil
}

// ToneStats counts photo crops by tone verdict.
func (s *Store) ToneStats(ctx context.Context) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT COALESCE(vtone,''), count(*)::int FROM catalog_crops WHERE kind='photo' GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// RefreshFamilyTones records on family_best the tone of the picture each card shows: the SKU
// finish of a vendor master, else the vision verdict (score ≥ 0.5) on the crop. Run after
// `catalogs vision --tone` and after family photos are re-picked.
func (s *Store) RefreshFamilyTones(ctx context.Context) (int64, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE family_best fb SET photo_tone = sub.tone FROM (
		SELECT fb.family_id, COALESCE(
		  (SELECT pi.vtone FROM product_images pi WHERE fb.web_path IS NOT NULL AND pi.path=fb.web_path AND pi.vtone IS NOT NULL LIMIT 1),
		  (SELECT cc.vtone FROM catalog_crops cc WHERE fb.web_path IS NULL AND fb.photo_path IS NOT NULL
		     AND (cc.path=fb.photo_path OR cc.hd_path=fb.photo_path) AND COALESCE(cc.vtonescore,0) >= 0.5 LIMIT 1)) AS tone
		FROM family_best fb) sub
		WHERE fb.family_id=sub.family_id AND fb.photo_tone IS DISTINCT FROM sub.tone`)
	if err != nil {
		return 0, err
	}
	n := t.RowsAffected()
	// when the camera is unsure, the finish of the SKU the crop is linked to says what the
	// picture shows (it is what the card's "shown in" label says, so ranking must agree)
	dict := s.finishDict(ctx)
	b := &pgx.Batch{}
	k := 0
	for _, e := range dict.Entries {
		if e.Head == "" {
			continue
		}
		b.Queue(`UPDATE family_best fb SET photo_tone=$2
			FROM catalog_crops cc, products p
			WHERE fb.photo_tone IS NULL AND fb.web_path IS NULL AND fb.photo_path IS NOT NULL
			  AND (cc.path=fb.photo_path OR cc.hd_path=fb.photo_path)
			  AND p.family_id=fb.family_id AND (p.sku=cc.sku OR (cc.base_n IS NOT NULL AND p.sku_n=cc.base_n)) AND p.finish=$1`,
			e.Name, ToneGroup(e.Head))
		k++
	}
	if k == 0 {
		return n, nil
	}
	res := s.Pool.SendBatch(ctx, b)
	defer res.Close()
	for i := 0; i < k; i++ {
		t, err := res.Exec()
		if err != nil {
			return n, err
		}
		n += t.RowsAffected()
	}
	return n, nil
}
