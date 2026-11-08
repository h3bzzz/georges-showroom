package assets

import (
	"image"
	"math"
)

// LQIP encodes a picture as one 20-bit integer the stylesheet decodes without JavaScript
// (Lean Rada's CSS-only LQIP): a base colour in Oklab (L 2 bits, a 3 bits, b 3 bits) and a
// 3x2 grid of luminance cells (2 bits each). Eight bytes of HTML per card, no script.
// Layout, most significant first: ll ll ll ll ll ll | L a a a b b b (cells row-major, L then a then b).
func LQIP(img image.Image) int {
	small := shrink(img, 24)
	b := small.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0
	}
	// base colour: the mean over the picture, in Oklab
	var rs, gs, bs float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl := rgb(small, x, y)
			rs, gs, bs = rs+r, gs+g, bs+bl
		}
	}
	n := float64(w * h)
	L, A, B := oklab(rs/n/255, gs/n/255, bs/n/255)
	// quantize: L in 0..1 → 2 bits; a,b in -0.4..0.4 (typical gamut) → 3 bits
	lq := clampInt(int(math.Round(L*3)), 0, 3)
	aq := clampInt(int(math.Round((A+0.4)/0.8*7)), 0, 7)
	bq := clampInt(int(math.Round((B+0.4)/0.8*7)), 0, 7)
	// cells: luminance relative to the base, 2 bits each (darker … lighter)
	var cells [6]int
	for cy := 0; cy < 2; cy++ {
		for cx := 0; cx < 3; cx++ {
			x0, x1 := cx*w/3, (cx+1)*w/3
			y0, y1 := cy*h/2, (cy+1)*h/2
			var lum, cnt float64
			for y := y0; y < max(y1, y0+1) && y < h; y++ {
				for x := x0; x < max(x1, x0+1) && x < w; x++ {
					r, g, bl := rgb(small, x, y)
					l, _, _ := oklab(r/255, g/255, bl/255)
					lum += l
					cnt++
				}
			}
			if cnt > 0 {
				lum /= cnt
			}
			// quantize absolute cell luminance to 0..3 (the CSS mixes it over the base colour)
			cells[cy*3+cx] = clampInt(int(math.Round(lum*3)), 0, 3)
		}
	}
	v := 0
	for _, c := range cells {
		v = v<<2 | c
	}
	v = v<<2 | lq
	v = v<<3 | aq
	v = v<<3 | bq
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// oklab converts sRGB (0..1) to Oklab.
func oklab(r, g, b float64) (L, a, bb float64) {
	r, g, b = lin(r), lin(g), lin(b)
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*b
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*b
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*b
	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	L = 0.2104542553*l + 0.7936177850*m - 0.0040720468*s
	a = 1.9779984951*l - 2.4285922050*m + 0.4505937099*s
	bb = 0.0259040371*l + 0.7827717662*m - 0.8086757660*s
	return
}

func lin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}
