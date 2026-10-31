package vision

import (
	"math"
	"math/rand"
)

// Probe is a multinomial logistic regression on CLIP embeddings (the "linear probe" of the
// CLIP paper): a few hundred hand labels beat prompt engineering on in-domain distinctions
// such as close-up vs whole product. Trained full-batch with L2, no dependencies.
type Probe struct {
	Names []string
	W     [][]float32 // class × dim
	B     []float32
}

// TrainProbe fits a probe on unit embeddings. Labels not in names are skipped.
func TrainProbe(names []string, embs [][]float32, labels []string, epochs int, lr, l2 float32) *Probe {
	idx := map[string]int{}
	for i, n := range names {
		idx[n] = i
	}
	var xs [][]float32
	var ys []int
	for i, l := range labels {
		if c, ok := idx[l]; ok {
			xs = append(xs, embs[i])
			ys = append(ys, c)
		}
	}
	if len(xs) == 0 {
		return nil
	}
	d := len(xs[0])
	p := &Probe{Names: names, W: make([][]float32, len(names)), B: make([]float32, len(names))}
	for c := range p.W {
		p.W[c] = make([]float32, d)
	}
	// unit embeddings give tiny logits; scale features so plain gradient descent converges
	const scale = 10
	gradW := make([][]float32, len(names))
	for c := range gradW {
		gradW[c] = make([]float32, d)
	}
	gradB := make([]float32, len(names))
	// class-balanced: a 20-example "detail" class must weigh as much as 300 photos
	count := make([]float32, len(names))
	for _, y := range ys {
		count[y]++
	}
	wgt := make([]float32, len(xs))
	for i, y := range ys {
		wgt[i] = float32(len(xs)) / (float32(len(names)) * count[y])
	}
	n := float32(len(xs))
	for ep := 0; ep < epochs; ep++ {
		for c := range gradW {
			clear(gradW[c])
		}
		clear(gradB)
		for i, x := range xs {
			pr := p.logits(x, scale)
			softmaxInPlace(pr)
			for c := range pr {
				g := pr[c]
				if c == ys[i] {
					g -= 1
				}
				g *= wgt[i] / n
				gradB[c] += g
				wc := gradW[c]
				for k := range x {
					wc[k] += g * x[k] * scale
				}
			}
		}
		for c := range p.W {
			wc, gc := p.W[c], gradW[c]
			for k := range wc {
				wc[k] -= lr * (gc[k] + l2*wc[k])
			}
			p.B[c] -= lr * gradB[c]
		}
	}
	return p
}

func (p *Probe) logits(x []float32, scale float32) []float32 {
	out := make([]float32, len(p.Names))
	for c := range p.W {
		out[c] = p.B[c] + scale*Dot(p.W[c], x)
	}
	return out
}

// Probs returns the class distribution for an embedding.
func (p *Probe) Probs(emb []float32) []float32 {
	pr := p.logits(emb, 10)
	softmaxInPlace(pr)
	return pr
}

func softmaxInPlace(v []float32) {
	mx := float32(math.Inf(-1))
	for _, x := range v {
		mx = max(mx, x)
	}
	var z float64
	for i, x := range v {
		e := math.Exp(float64(x - mx))
		v[i] = float32(e)
		z += e
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / z)
	}
}

// CrossValidate trains the probe on k folds and reports held-out predictions through the
// head's mixing (probeWeight of the probe distribution, the rest from prompts).
func (h *Head) CrossValidate(embs [][]float32, truth, paths []string, k int, probeWeight float32, seed int64) Eval {
	ev := Eval{Mix: probeWeight, Confusion: map[[2]string]int{}}
	savedProbe, savedW, savedMix := h.Probe, h.ProbeWeight, h.Mix
	h.Mix = 0
	defer func() { h.Probe, h.ProbeWeight, h.Mix = savedProbe, savedW, savedMix }()
	perm := rand.New(rand.NewSource(seed)).Perm(len(embs))
	fold := make([]int, len(embs))
	for i, p := range perm {
		fold[p] = i % k
	}
	for f := 0; f < k; f++ {
		var trX [][]float32
		var trY []string
		for i := range embs {
			if fold[i] != f {
				trX = append(trX, embs[i])
				trY = append(trY, truth[i])
			}
		}
		probe := TrainProbe(h.Names, trX, trY, 300, 0.5, 1e-3)
		for i := range embs {
			if fold[i] != f {
				continue
			}
			h.Probe, h.ProbeWeight = probe, probeWeight
			pred, prob, _ := h.Classify(embs[i])
			ev.record(truth[i], pred, prob, paths[i])
		}
	}
	return ev
}
