package vision

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Index is the in-memory look-alike index: one quantized CLIP vector per picture a family
// shows (card and gallery), scored by dot product. 40k vectors of 512 int8 is 20 MB; a full
// scan takes a few milliseconds, so no approximate structure is needed yet.
type Index struct {
	mu     sync.RWMutex
	dim    int
	vecs   []int8    // n × dim
	scale  []float32 // per-vector dequantization factor
	family []int64   // per-vector family id
	asset  []int64
	byFam  map[int64][]int // family → vector rows (first is the card)
	loaded time.Time
}

// Hit is one index match.
type Hit struct {
	FamilyID int64
	AssetID  int64
	Score    float32 // cosine similarity
}

// Load reads the vectors of every pictured family from the database.
func Load(ctx context.Context, pool *pgxpool.Pool) (*Index, error) {
	rows, err := pool.Query(ctx, `
		SELECT fm.family_id, a.id, a.embedding
		FROM family_media fm JOIN assets a ON a.id=fm.asset_id
		WHERE fm.role IN ('card','gallery') AND a.embedding IS NOT NULL AND NOT a.rejected
		ORDER BY fm.family_id, (fm.role='card') DESC, fm.rank`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ix := &Index{byFam: map[int64][]int{}}
	for rows.Next() {
		var fid, aid int64
		var raw []byte
		if err := rows.Scan(&fid, &aid, &raw); err != nil {
			return nil, err
		}
		v := decode(raw)
		if len(v) == 0 {
			continue
		}
		if ix.dim == 0 {
			ix.dim = len(v)
		}
		if len(v) != ix.dim {
			continue
		}
		ix.add(fid, aid, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ix.loaded = time.Now()
	return ix, nil
}

func (ix *Index) add(fid, aid int64, v []float32) {
	q, s := quantize(v)
	ix.byFam[fid] = append(ix.byFam[fid], len(ix.family))
	ix.vecs = append(ix.vecs, q...)
	ix.scale = append(ix.scale, s)
	ix.family = append(ix.family, fid)
	ix.asset = append(ix.asset, aid)
}

// Len is the number of vectors.
func (ix *Index) Len() int { return len(ix.family) }

// Dim is the vector length.
func (ix *Index) Dim() int { return ix.dim }

// Search returns the n best families for a unit query vector (best picture per family).
func (ix *Index) Search(q []float32, n int, exclude int64) []Hit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if len(q) != ix.dim || n <= 0 {
		return nil
	}
	qq, qs := quantize(q)
	best := map[int64]Hit{}
	for i := range ix.family {
		fid := ix.family[i]
		if fid == exclude {
			continue
		}
		row := ix.vecs[i*ix.dim : (i+1)*ix.dim]
		var acc int32
		for j, x := range row {
			acc += int32(x) * int32(qq[j])
		}
		s := float32(acc) * ix.scale[i] * qs
		if h, ok := best[fid]; !ok || s > h.Score {
			best[fid] = Hit{fid, ix.asset[i], s}
		}
	}
	hits := make([]Hit, 0, len(best))
	for _, h := range best {
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// Similar finds the families that look like a family's card picture.
func (ix *Index) Similar(fid int64, n int) ([]Hit, error) {
	ix.mu.RLock()
	rows := ix.byFam[fid]
	if len(rows) == 0 {
		ix.mu.RUnlock()
		return nil, fmt.Errorf("family %d has no embedded picture", fid)
	}
	i := rows[0]
	q := make([]float32, ix.dim)
	for j, x := range ix.vecs[i*ix.dim : (i+1)*ix.dim] {
		q[j] = float32(x) * ix.scale[i]
	}
	ix.mu.RUnlock()
	return ix.Search(Normalize(q), n, fid), nil
}

// quantize maps a vector to int8 with one scale (max-abs). Cosine error stays under 1 %.
func quantize(v []float32) ([]int8, float32) {
	var m float32
	for _, x := range v {
		if a := float32(math.Abs(float64(x))); a > m {
			m = a
		}
	}
	if m == 0 {
		return make([]int8, len(v)), 0
	}
	s := m / 127
	out := make([]int8, len(v))
	for i, x := range v {
		out[i] = int8(math.Round(float64(x / s)))
	}
	return out, s
}

// decode reads a float32 little-endian vector as stored in assets.embedding.
func decode(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// Encode writes a vector the way decode reads it.
func Encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}
