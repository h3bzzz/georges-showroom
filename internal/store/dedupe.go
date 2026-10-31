package store

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"

	"georges_/internal/vision"
)

// DupThreshold is the cosine similarity above which two crops of like shape are the same shot.
// CLIP ViT-B/16 embeddings of the same photograph re-cut from another page sit above 0.97;
// two different products of one collection sit around 0.85–0.93.
const DupThreshold = 0.97

type dupCrop struct {
	id   int64
	w, h int
	emb  []float32
}

// DedupeCrops marks, within each brand, every crop that repeats a larger crop of the same
// photograph (dup_of = the larger one's id) and clears the mark on crops that no longer do.
// Only crops with an embedding take part; kinds are compared like with like. Returns the
// number of crops marked.
func (s *Store) DedupeCrops(ctx context.Context, brandCode string) (int64, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.brand_code, cc.id, cc.w, cc.h, cc.kind, cc.vemb FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		WHERE cc.vemb IS NOT NULL AND ($1='' OR c.brand_code=$1) ORDER BY c.brand_code, cc.kind, cc.w*cc.h DESC, cc.id`, brandCode)
	if err != nil {
		return 0, err
	}
	groups := map[string][]dupCrop{}
	var order []string
	for rows.Next() {
		var code, kind string
		var raw []byte
		var c dupCrop
		if err := rows.Scan(&code, &c.id, &c.w, &c.h, &kind, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		c.emb = DecodeEmbedding(raw)
		key := code + "/" + kind
		if groups[key] == nil {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}
	rows.Close()
	dups := map[int64]int64{}
	for _, key := range order {
		for _, d := range groupDups(groups[key]) {
			dups[d[0]] = d[1]
		}
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE catalog_crops cc SET dup_of = NULL FROM catalogs c WHERE c.id=cc.catalog_id AND ($1='' OR c.brand_code=$1) AND cc.dup_of IS NOT NULL`, brandCode); err != nil {
		return 0, err
	}
	b := &pgx.Batch{}
	for id, of := range dups {
		b.Queue(`UPDATE catalog_crops SET dup_of=$2 WHERE id=$1`, id, of)
	}
	if err := s.Pool.SendBatch(ctx, b).Close(); err != nil {
		return 0, err
	}
	return int64(len(dups)), nil
}

// groupDups returns [dup, kept] pairs: crops are visited largest first, and one that matches
// an already kept crop in shape (aspect within 15 %) and embedding (cosine ≥ DupThreshold)
// is a duplicate of it.
func groupDups(crops []dupCrop) [][2]int64 {
	sort.SliceStable(crops, func(i, j int) bool { return crops[i].w*crops[i].h > crops[j].w*crops[j].h })
	var kept []dupCrop
	var out [][2]int64
	for _, c := range crops {
		if c.w == 0 || c.h == 0 || len(c.emb) == 0 {
			continue
		}
		asp := float64(c.w) / float64(c.h)
		dup := false
		for _, k := range kept {
			ka := float64(k.w) / float64(k.h)
			if asp > ka*1.15 || asp < ka/1.15 {
				continue
			}
			if vision.Dot(c.emb, k.emb) >= DupThreshold {
				out = append(out, [2]int64{c.id, k.id})
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, c)
		}
	}
	return out
}
