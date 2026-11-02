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
	// a printed frame (a hairline box around the photo, California Faucets) joins the caption
	// to the product so no blank gap separates them; the frame is peeled off first and the
	// profiles are read inside it
	in := stripFrame(img)
	cols := make([]int, in.Dx())
	rows := make([]int, in.Dy())
	total := 0
	for y := in.Min.Y; y < in.Max.Y; y++ {
		for x := in.Min.X; x < in.Max.X; x++ {
			if ink, _, _ := isInk(img, x, y); ink {
				cols[x-in.Min.X]++
				rows[y-in.Min.Y]++
				total++
			}
		}
	}
	// a caption touching the top or bottom edge sits only a row or two above the product on a
	// tight crop (a 127 px "…ed Handle" over a faucet): any blank row separates it when the
	// band is light, so it is blanked out before the gap search
	capTop, capBot := edgeCaption(rows, in.Dx(), total)
	x0, x1 := mainSegment(cols, in.Dy(), max(3, in.Dx()*2/100))
	y0, y1 := mainSegment(rows, in.Dx(), max(3, in.Dy()*2/100))
	if x0 < 0 || y0 < 0 {
		return b, false
	}
	x0, x1, y0, y1 = x0+in.Min.X, x1+in.Min.X, y0+in.Min.Y, y1+in.Min.Y
	pad := max(2, min(w, h)/40)
	keep = image.Rect(max(in.Min.X, x0-pad), max(in.Min.Y+capTop, y0-pad), min(in.Max.X, x1+pad), min(in.Max.Y-capBot, y1+pad))
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

// edgeCaption zeroes a light ink band (under a quarter of the crop's ink) that touches the
// first or last row and ends in a blank row within the outer 18% of the height. It returns
// how many rows were cut at the top and at the bottom, so padding never reaches back in.
func edgeCaption(rows []int, width, total int) (top, bottom int) {
	h := len(rows)
	thr := max(1, width/100)
	lim := max(4, h*18/100)
	cut := func(idx func(i int) int) int {
		mass := 0
		for i := 0; i < lim; i++ {
			y := idx(i)
			if rows[y] <= thr {
				if i == 0 || mass == 0 || mass*4 > total {
					return 0
				}
				for j := 0; j < i; j++ {
					rows[idx(j)] = 0
				}
				return i
			}
			mass += rows[y]
		}
		return 0
	}
	top = cut(func(i int) int { return i })
	bottom = cut(func(i int) int { return h - 1 - i })
	return top, bottom
}

// stripFrame returns the box inside any hairline frame drawn along the crop's edges: an edge
// row or column is frame when at least four fifths of it is ink, scanning at most a tenth of
// the way in; one more pixel of margin is dropped past the line.
func stripFrame(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rowInk := func(y int) int {
		n := 0
		for x := 0; x < w; x++ {
			if ink, _, _ := isInk(img, x, y); ink {
				n++
			}
		}
		return n
	}
	colInk := func(x int) int {
		n := 0
		for y := 0; y < h; y++ {
			if ink, _, _ := isInk(img, x, y); ink {
				n++
			}
		}
		return n
	}
	in := image.Rect(0, 0, w, h)
	lim := max(2, min(w, h)/10)
	for y := 0; y < lim && rowInk(y)*5 >= w*4; y++ {
		in.Min.Y = y + 2
	}
	for y := h - 1; y >= h-lim && rowInk(y)*5 >= w*4; y-- {
		in.Max.Y = y - 1
	}
	for x := 0; x < lim && colInk(x)*5 >= h*4; x++ {
		in.Min.X = x + 2
	}
	for x := w - 1; x >= w-lim && colInk(x)*5 >= h*4; x-- {
		in.Max.X = x - 1
	}
	if in.Dx() < 20 || in.Dy() < 20 {
		return image.Rect(0, 0, w, h)
	}
	return in
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
