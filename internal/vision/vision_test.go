package vision

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestPreparePadsToSquareOnWhite(t *testing.T) {
	tall := image.NewRGBA(image.Rect(0, 0, 10, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 10; x++ {
			tall.Set(x, y, color.Black)
		}
	}
	out := Prepare(tall)
	if out.Bounds().Dx() != Side || out.Bounds().Dy() != Side {
		t.Fatalf("size %v", out.Bounds())
	}
	// the black bar sits in the middle quarter; the margins are white
	if r, _, _, _ := out.At(5, Side/2).RGBA(); r>>8 < 250 {
		t.Fatalf("left margin not white: %d", r>>8)
	}
	if r, _, _, _ := out.At(Side/2, Side/2).RGBA(); r>>8 > 10 {
		t.Fatalf("centre not black: %d", r>>8)
	}
}

func TestSoftmaxAndMean(t *testing.T) {
	a := Normalize([]float32{1, 0})
	b := Normalize([]float32{0, 1})
	p := softmax([][]float32{a, b}, Normalize([]float32{1, 0.1}), 100)
	if p[0] < 0.99 {
		t.Fatalf("expected class 0 to dominate, got %v", p)
	}
	if s := p[0] + p[1]; math.Abs(float64(s)-1) > 1e-5 {
		t.Fatalf("not normalised: %v", s)
	}
	m := Mean([][]float32{{1, 0}, {0, 1}})
	if math.Abs(float64(m[0]-m[1])) > 1e-6 || math.Abs(float64(Dot(m, m))-1) > 1e-5 {
		t.Fatalf("mean %v", m)
	}
}

func TestLeaveOneOutUsesOtherExamples(t *testing.T) {
	h := &Head{Names: []string{"x", "y"}, Text: [][]float32{Normalize([]float32{1, 0, 0}), Normalize([]float32{0, 1, 0})}, Proto: make([][]float32, 2), Temp: 100}
	// embeddings cluster on the third axis in a way the text head cannot see
	embs := [][]float32{
		Normalize([]float32{0.1, 0, 1}), Normalize([]float32{0.1, 0, 0.9}), Normalize([]float32{0.1, 0.05, 1}),
		Normalize([]float32{0.1, 0, -1}), Normalize([]float32{0.1, 0, -0.9}), Normalize([]float32{0.1, 0.05, -1}),
	}
	truth := []string{"x", "x", "x", "y", "y", "y"}
	paths := []string{"1", "2", "3", "4", "5", "6"}
	zero := h.LeaveOneOut(embs, truth, paths, 0, 20)
	few := h.LeaveOneOut(embs, truth, paths, 1, 20)
	if zero.Correct != 3 {
		t.Fatalf("zero-shot should get the x half only, got %d", zero.Correct)
	}
	if few.Correct != 6 {
		t.Fatalf("prototypes should separate the clusters, got %d: %v", few.Correct, few.Misses)
	}
	if h.Mix != 0 || h.Proto[0] != nil {
		t.Fatalf("LeaveOneOut must restore the head")
	}
}

func TestProbeSeparatesClusters(t *testing.T) {
	var embs [][]float32
	var labels []string
	add := func(n int, label string, axis int) {
		for i := 0; i < n; i++ {
			v := []float32{0, 0, 0}
			v[axis] = 1
			v[(axis+1)%3] = float32(i%5) * 0.01
			embs = append(embs, Normalize(v))
			labels = append(labels, label)
		}
	}
	add(30, "a", 0)
	add(30, "b", 1)
	add(3, "c", 2) // the minority class the balanced loss must not give up on
	p := TrainProbe([]string{"a", "b", "c"}, embs, labels, 300, 0.5, 1e-3)
	for i, e := range embs {
		pr := p.Probs(e)
		best := 0
		for c := range pr {
			if pr[c] > pr[best] {
				best = c
			}
		}
		if p.Names[best] != labels[i] {
			t.Fatalf("example %d: want %s got %v", i, labels[i], pr)
		}
	}
	h := &Head{Names: p.Names, Text: [][]float32{embs[0], embs[30], embs[60]}, Temp: 100, Probe: p, ProbeWeight: 0.8}
	if name, prob, _ := h.Classify(embs[61]); name != "c" || prob < 0.8 {
		t.Fatalf("mixed head: %s %.2f", name, prob)
	}
}
