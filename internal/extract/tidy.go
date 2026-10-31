package extract

import (
	"image"
)

// Tidy finds the main object inside a crop and returns the box to keep: edge slivers of a
// neighbouring picture and caption bands (the SKU printed under the photo) are separated from
// the object by a blank gap, so the column and row ink profiles are split at gaps and only the
// heaviest segment survives. Satellite segments are dropped only when they are light (under a
// quarter of the main segment's ink) so a two-part product (faucet with a separate sprayer)
// stays whole. ok is false when nothing needs to change or the result would be suspiciously
// small.
func Tidy(src image.Image) (keep image.Rectangle, ok bool) {
	img := rgbaOf(src)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 24 || h < 24 {
		return b, false
	}
	cols := make([]int, w)
	rows := make([]int, h)
	total := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if in, _, _ := isInk(img, x, y); in {
				cols[x]++
				rows[y]++
				total++
			}
		}
	}
	x0, x1 := mainSegment(cols, h, max(3, w*2/100))
	y0, y1 := mainSegment(rows, w, max(3, h*2/100))
	if x0 < 0 || y0 < 0 {
		return b, false
	}
	pad := max(2, min(w, h)/40)
	keep = image.Rect(max(0, x0-pad), max(0, y0-pad), min(w, x1+pad), min(h, y1+pad))
	kept := 0
	for y := keep.Min.Y; y < keep.Max.Y; y++ {
		for x := keep.Min.X; x < keep.Max.X; x++ {
			if in, _, _ := isInk(img, x, y); in {
				kept++
			}
		}
	}
	if total == 0 || kept*100 < total*60 || keep.Dx() < 20 || keep.Dy() < 20 {
		return b, false // would throw away much of the picture: leave it alone
	}
	if keep.Min.X <= 1 && keep.Min.Y <= 1 && keep.Max.X >= w-1 && keep.Max.Y >= h-1 {
		return b, false
	}
	return keep, true
}

// mainSegment splits a 1-D ink profile at blank gaps (gapLen consecutive bins with ink under
// 1% of the other dimension) and returns the extent of the heaviest segment merged with any
// neighbour segment that is at least a quarter as heavy.
func mainSegment(prof []int, other, gapLen int) (int, int) {
	thr := max(1, other/100)
	type seg struct{ a, b, mass int }
	var segs []seg
	cur := seg{a: -1}
	blank := 0
	for i, v := range prof {
		if v <= thr {
			blank++
			if cur.a >= 0 && blank >= gapLen {
				segs = append(segs, cur)
				cur = seg{a: -1}
			}
			continue
		}
		blank = 0
		if cur.a < 0 {
			cur = seg{a: i}
		}
		cur.b = i
		cur.mass += v
	}
	if cur.a >= 0 {
		segs = append(segs, cur)
	}
	if len(segs) == 0 {
		return -1, -1
	}
	best := 0
	for i, s := range segs {
		if s.mass > segs[best].mass {
			best = i
		}
	}
	lo, hi := best, best
	for lo > 0 && segs[lo-1].mass*4 >= segs[best].mass {
		lo--
	}
	for hi < len(segs)-1 && segs[hi+1].mass*4 >= segs[best].mass {
		hi++
	}
	return segs[lo].a, segs[hi].b + 1
}
