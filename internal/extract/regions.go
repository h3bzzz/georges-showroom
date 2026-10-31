// Package extract finds product photos on catalog page renders and links them to nearby SKUs.
//
// Heuristic v1: downscale, mask non-white pixels, erase OCR word boxes (dilated), take the
// remaining connected components above a minimum area, then attach each region to the closest
// SKU-looking word below or beside it.
package extract

import (
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"sort"

	"golang.org/x/image/draw"

	"georges_/internal/ocr"
)

type Rect struct{ X, Y, W, H int }

// Region is a candidate image with a few cheap descriptors used to classify it.
type Region struct {
	Rect
	Fill     float64 // fraction of the box that is non-white
	Tones    int     // distinct luminance buckets (of 16) among non-white pixels
	Solid    float64 // fraction of non-white pixels whose 4 neighbours are also non-white (thin strokes → low)
	Dominant float64 // share of the most common tone bucket
	Kind     string  // photo | drawing
}

func (r Rect) Area() int   { return r.W * r.H }
func (r Rect) cx() float64 { return float64(r.X) + float64(r.W)/2 }
func (r Rect) cy() float64 { return float64(r.Y) + float64(r.H)/2 }

type Options struct {
	Scale       int     // downscale factor (4 = analyse at 1/4 size)
	MinAreaFrac float64 // min region area as fraction of page area (e.g. 0.004)
	MaxAreaFrac float64 // ignore regions bigger than this (full-page backgrounds)
	WhiteThresh uint8   // channel value above which a pixel counts as white
	Pad         int     // dilation around text boxes (full-res px)
	MaxDist     float64 // max centre distance (full-res px) between region and SKU word
}

func Defaults() Options {
	return Options{Scale: 4, MinAreaFrac: 0.003, MaxAreaFrac: 0.6, WhiteThresh: 235, Pad: 6, MaxDist: 900}
}

// Load decodes a JPEG.
func Load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}

// Regions returns candidate image regions in full-resolution coordinates. Regions with very
// few tones (big display letters, rules, boxes) are discarded; few-tone or text-laden regions
// are tagged as line drawings (dimension sketches); the rest are photos.
func Regions(img image.Image, words []ocr.Word, o Options) []Region {
	b := img.Bounds()
	W, H := b.Dx()/o.Scale, b.Dy()/o.Scale
	if W < 4 || H < 4 {
		return nil
	}
	small := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, draw.Src, nil)

	mask := make([]bool, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			c := small.RGBAAt(x, y)
			if c.R < o.WhiteThresh || c.G < o.WhiteThresh || c.B < o.WhiteThresh {
				mask[y*W+x] = true
			}
		}
	}
	// erase text
	for _, w := range words {
		x0 := (w.X - o.Pad) / o.Scale
		y0 := (w.Y - o.Pad) / o.Scale
		x1 := (w.X + w.W + o.Pad) / o.Scale
		y1 := (w.Y + w.H + o.Pad) / o.Scale
		for y := max(0, y0); y <= min(H-1, y1); y++ {
			for x := max(0, x0); x <= min(W-1, x1); x++ {
				mask[y*W+x] = false
			}
		}
	}
	// close small gaps: dilate once then components
	dil := make([]bool, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if !mask[y*W+x] {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					yy, xx := y+dy, x+dx
					if yy >= 0 && yy < H && xx >= 0 && xx < W {
						dil[yy*W+xx] = true
					}
				}
			}
		}
	}
	seen := make([]bool, W*H)
	var regs []Region
	pageArea := float64(W * H)
	stack := make([]int, 0, 1024)
	for i := range dil {
		if !dil[i] || seen[i] {
			continue
		}
		minx, miny, maxx, maxy, n := W, H, 0, 0, 0
		var buckets [16]int
		inked, interior := 0, 0
		stack = append(stack[:0], i)
		seen[i] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%W, p/W
			n++
			if mask[p] {
				inked++
				c := small.RGBAAt(x, y)
				lum := (int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000
				buckets[lum/16]++
				if x > 0 && y > 0 && x < W-1 && y < H-1 && mask[p-1] && mask[p+1] && mask[p-W] && mask[p+W] {
					interior++
				}
			}
			minx, miny = min(minx, x), min(miny, y)
			maxx, maxy = max(maxx, x), max(maxy, y)
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				xx, yy := x+d[0], y+d[1]
				if xx < 0 || yy < 0 || xx >= W || yy >= H {
					continue
				}
				q := yy*W + xx
				if dil[q] && !seen[q] {
					seen[q] = true
					stack = append(stack, q)
				}
			}
		}
		bw, bh := maxx-minx+1, maxy-miny+1
		frac := float64(bw*bh) / pageArea
		if frac < o.MinAreaFrac || frac > o.MaxAreaFrac || bw < 12 || bh < 12 {
			continue
		}
		fill := float64(n) / float64(bw*bh)
		if fill < 0.18 { // mostly hollow: table grid, rule, outlined box
			continue
		}
		tones, top := 0, 0
		minCount := max(3, n/200)
		for _, b := range buckets {
			if b >= minCount {
				tones++
			}
			top = max(top, b)
		}
		if inked == 0 {
			continue
		}
		solid := float64(interior) / float64(inked)
		dominant := float64(top) / float64(inked)
		r := Region{Rect: Rect{X: minx * o.Scale, Y: miny * o.Scale, W: bw * o.Scale, H: bh * o.Scale},
			Fill: fill, Tones: tones, Solid: solid, Dominant: dominant, Kind: "photo"}
		inside := wordsInside(r.Rect, words)
		switch {
		case tones < 4 || dominant >= 0.9:
			continue // flat single-tone shapes: rules, swatches, colour banners, boxes
		case solid < 0.45 && dominant >= 0.8:
			continue // thin uniform strokes: display lettering, outlined shapes
		case inside >= 2 || solid < 0.5 || tones <= 7 || fill < 0.16:
			r.Kind = "drawing" // dimension sketches, line art, technical drawings
		}
		ar := float64(bw) / float64(bh)
		if ar > 5 || ar < 0.2 {
			continue // long thin strips
		}
		regs = append(regs, r)
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].Area() > regs[j].Area() })
	return regs
}

// wordsInside counts OCR words (dimension labels like 11-1/2") whose box lies well inside r.
func wordsInside(r Rect, words []ocr.Word) int {
	n := 0
	mx, my := r.W/10, r.H/10
	for _, w := range words {
		if w.Conf < 40 || len(w.Text) < 2 {
			continue
		}
		if w.X > r.X+mx && w.X+w.W < r.X+r.W-mx && w.Y > r.Y+my && w.Y+w.H < r.Y+r.H-my {
			n++
		}
	}
	return n
}

type Match struct {
	Region Region
	SKU    string
	Word   ocr.Word
	Dist   float64
	Score  float64
}

// LinkSKUs pairs regions with the closest SKU-looking word, preferring words below the image.
func LinkSKUs(regs []Region, words []ocr.Word, o Options) []Match {
	var skus []ocr.Word
	for _, w := range words {
		if ocr.LooksLikeSKU(w.Text) && w.Conf >= 60 {
			skus = append(skus, w)
		}
	}
	var out []Match
	for _, r := range regs {
		best := Match{Region: r, Dist: math.Inf(1)}
		for _, w := range skus {
			wx, wy := float64(w.X)+float64(w.W)/2, float64(w.Y)+float64(w.H)/2
			dx := wx - r.cx()
			dy := wy - r.cy()
			// words below the image are the common layout: shrink vertical distance a bit,
			// words above cost more.
			if dy > 0 {
				dy *= 0.7
			} else {
				dy *= 1.6
			}
			d := math.Hypot(dx, dy)
			if d < best.Dist {
				best = Match{Region: r, SKU: ocr.Normalize(w.Text), Word: w, Dist: d}
			}
		}
		if best.SKU == "" || best.Dist > o.MaxDist {
			continue
		}
		best.Score = 1 - best.Dist/o.MaxDist
		out = append(out, best)
	}
	return out
}

// Crop writes region r of img (with margin) to path as JPEG.
func Crop(img image.Image, r Rect, margin int, path string) (int, int, error) {
	b := img.Bounds()
	x0, y0 := max(b.Min.X, r.X-margin), max(b.Min.Y, r.Y-margin)
	x1, y1 := min(b.Max.X, r.X+r.W+margin), min(b.Max.Y, r.Y+r.H+margin)
	sub := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(sub, sub.Bounds(), img, image.Pt(x0, y0), draw.Src)
	f, err := os.Create(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return x1 - x0, y1 - y0, jpeg.Encode(f, sub, &jpeg.Options{Quality: 90})
}

var _ = color.White

// TrimText shrinks r so that recognized words sitting in its top or bottom band (captions,
// labels) are excluded. Words fully inside the middle are left alone (they are part of the
// picture, e.g. dimension labels on a drawing).
func TrimText(r Rect, words []ocr.Word, pad int) Rect {
	top, bottom := r.Y, r.Y+r.H
	left, right := r.X, r.X+r.W
	for _, w := range words {
		wx0, wy0, wx1, wy1 := w.X, w.Y, w.X+w.W, w.Y+w.H
		if wx1 < r.X || wx0 > r.X+r.W || wy1 < r.Y || wy0 > r.Y+r.H {
			continue
		}
		switch {
		case wy1 <= r.Y+r.H*28/100: // caption above / at the top
			if wy1+pad > top {
				top = wy1 + pad
			}
		case wy0 >= r.Y+r.H*72/100: // caption below
			if wy0-pad < bottom {
				bottom = wy0 - pad
			}
		case wx1 <= r.X+r.W*20/100: // label column at the left
			if wx1+pad > left {
				left = wx1 + pad
			}
		case wx0 >= r.X+r.W*80/100:
			if wx0-pad < right {
				right = wx0 - pad
			}
		}
	}
	if bottom-top < r.H/3 || right-left < r.W/3 {
		return r // trimming would destroy the region; keep original
	}
	return Rect{X: left, Y: top, W: right - left, H: bottom - top}
}
