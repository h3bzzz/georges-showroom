package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
)

// CropRef is a crop to embed.
type CropRef struct {
	ID   int64
	Path string
}

// CropsToEmbed lists crops without an embedding from this model (or all of them).
func (s *Store) CropsToEmbed(ctx context.Context, brand, model string, all bool, limit int) ([]CropRef, error) {
	q := `SELECT cc.id, cc.path FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		WHERE ($1='' OR c.brand_code=$1) AND ($2 OR cc.vemb IS NULL OR cc.vmodel IS DISTINCT FROM $3)
		ORDER BY cc.catalog_id, cc.page, cc.id`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.Pool.Query(ctx, q, brand, all, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CropRef
	for rows.Next() {
		var c CropRef
		if err := rows.Scan(&c.ID, &c.Path); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CropVision is one crop's model verdict.
type CropVision struct {
	ID       int64
	Emb      []float32 // nil to keep the stored embedding (reclassify)
	Kind     string
	Score    float32
	Subject  string
	SubScore float32
}

// SaveCropVision stores verdicts and recomputes the effective kind of each crop.
func (s *Store) SaveCropVision(ctx context.Context, model string, rows []CropVision) error {
	b := &pgx.Batch{}
	for _, r := range rows {
		if r.Emb != nil {
			b.Queue(`UPDATE catalog_crops SET vemb=$2, vmodel=$3, vkind=$4, vscore=$5, vsubject=$6, vsubscore=$7,
				kind=crop_kind(COALESCE(pkind, kind), $4, $5) WHERE id=$1`,
				r.ID, EncodeEmbedding(r.Emb), model, r.Kind, r.Score, r.Subject, r.SubScore)
		} else {
			b.Queue(`UPDATE catalog_crops SET vkind=$2, vscore=$3, vsubject=$4, vsubscore=$5,
				kind=crop_kind(COALESCE(pkind, kind), $2, $3) WHERE id=$1`,
				r.ID, r.Kind, r.Score, r.Subject, r.SubScore)
		}
	}
	return s.Pool.SendBatch(ctx, b).Close()
}

// CropEmbedding is a stored embedding.
type CropEmbedding struct {
	ID   int64
	Path string
	Emb  []float32
}

// CropEmbeddings streams stored embeddings for a model (optionally one brand) to fn.
func (s *Store) CropEmbeddings(ctx context.Context, brand, model string, fn func(CropEmbedding) error) error {
	rows, err := s.Pool.Query(ctx, `SELECT cc.id, cc.path, cc.vemb FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		WHERE cc.vemb IS NOT NULL AND cc.vmodel=$2 AND ($1='' OR c.brand_code=$1) ORDER BY cc.id`, brand, model)
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

// RecomputeCropKinds re-derives kind from pkind and the vision verdict for every crop
// (after a crop_kind() threshold change) and returns how many changed.
func (s *Store) RecomputeCropKinds(ctx context.Context) (int64, error) {
	t, err := s.Pool.Exec(ctx, `UPDATE catalog_crops SET kind = crop_kind(COALESCE(pkind, kind), vkind, vscore)
		WHERE kind IS DISTINCT FROM crop_kind(COALESCE(pkind, kind), vkind, vscore)`)
	if err != nil {
		return 0, err
	}
	return t.RowsAffected(), nil
}

// VisionStats counts crops by effective kind and by vision verdict.
func (s *Store) VisionStats(ctx context.Context) (kinds, verdicts map[string]int, embedded int, err error) {
	kinds, verdicts = map[string]int{}, map[string]int{}
	rows, err := s.Pool.Query(ctx, `SELECT kind, count(*) FROM catalog_crops GROUP BY 1`)
	if err != nil {
		return nil, nil, 0, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		kinds[k] = n
	}
	rows.Close()
	rows, err = s.Pool.Query(ctx, `SELECT COALESCE(vkind,'-')||'/'||COALESCE(vsubject,'-'), count(*) FROM catalog_crops WHERE vkind IS NOT NULL GROUP BY 1`)
	if err != nil {
		return nil, nil, 0, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		verdicts[k] = n
		embedded += n
	}
	rows.Close()
	return kinds, verdicts, embedded, rows.Err()
}

// EncodeEmbedding packs float32s little-endian.
func EncodeEmbedding(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// DecodeEmbedding unpacks EncodeEmbedding's output.
func DecodeEmbedding(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}
