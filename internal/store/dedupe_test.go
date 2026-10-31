package store

import "testing"

func TestGroupDups(t *testing.T) {
	a := []float32{1, 0, 0}
	near := []float32{0.995, 0.0998, 0} // cosine ≈ 0.995
	far := []float32{0.7, 0.714, 0}     // cosine ≈ 0.7
	crops := []dupCrop{
		{id: 1, w: 100, h: 100, emb: a},
		{id: 2, w: 400, h: 400, emb: near}, // larger copy of the same shot: kept, 1 points at it
		{id: 3, w: 200, h: 100, emb: near}, // same embedding, different shape: kept
		{id: 4, w: 300, h: 300, emb: far},  // different product: kept
	}
	got := map[int64]int64{}
	for _, d := range groupDups(crops) {
		got[d[0]] = d[1]
	}
	if len(got) != 1 || got[1] != 2 {
		t.Errorf("dups = %v, want {1:2}", got)
	}
}
