package extract

import (
	"image"
	"image/draw"
	"math"
)

// PixelStats summarises a crop for the audit.
//
//	Ink       non-white share of pixels
//	Midtone   share of ink pixels with mid luminance (photographs shade; text and line art are black)
//	Sat       mean saturation of ink pixels
//	Thin      share of ink pixels that touch a white pixel: line art and text ≈ 1, solid objects ≈ 0.2
//	BlankRows share of pixel rows with (almost) no ink: text and tables have gaps between lines
//	RuleRows  pixel rows that are ink across ≥ 60% of the width: table rules
type PixelStats struct {
	Ink, Midtone, Sat, Thin, BlankRows float64
	Light, Dark                        float64 // share of ink pixels that are light-gray fill / near-black
	RuleRows                           int
	Aspect                             float64 // width / height
}

// rgbaOf returns img as packed RGBA so the sampler can index pixels directly; the generic
// image.At path costs an interface call and a colour conversion per pixel, which made the
// audit of 44k crops CPU-bound.
func rgbaOf(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	r := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(r, r.Bounds(), img, b.Min, draw.Src)
	return r
}

func isInk(img *image.RGBA, x, y int) (ink bool, luma, sat float64) {
	i := img.PixOffset(x, y)
	rf, gf, bf := float64(img.Pix[i]), float64(img.Pix[i+1]), float64(img.Pix[i+2])
	mx := math.Max(rf, math.Max(gf, bf))
	mn := math.Min(rf, math.Min(gf, bf))
	luma = 0.299*rf + 0.587*gf + 0.114*bf
	if mx > 0 {
		sat = (mx - mn) / mx
	}
	return luma < 235 || sat > 0.10, luma, sat
}

// Stats samples every step-th pixel.
func Stats(src image.Image, step int) PixelStats {
	if step < 1 {
		step = 1
	}
	img := rgbaOf(src)
	b := img.Bounds()
	var n, ink, mid, thin, rows, blank, rule, light, dark int
	var satSum float64
	for y := b.Min.Y; y < b.Max.Y; y += step {
		rowN, rowInk := 0, 0
		for x := b.Min.X; x < b.Max.X; x += step {
			n++
			rowN++
			in, luma, sat := isInk(img, x, y)
			if !in {
				continue
			}
			ink++
			rowInk++
			satSum += sat
			switch {
			case luma > 60 && luma < 205:
				mid++
			case luma >= 205:
				light++
			default:
				dark++
			}
			// touches white?
			for _, d := range [4][2]int{{step, 0}, {-step, 0}, {0, step}, {0, -step}} {
				nx, ny := x+d[0], y+d[1]
				if nx < b.Min.X || nx >= b.Max.X || ny < b.Min.Y || ny >= b.Max.Y {
					continue
				}
				if w, _, _ := isInk(img, nx, ny); !w {
					thin++
					break
				}
			}
		}
		rows++
		if rowN > 0 {
			if float64(rowInk) < 0.015*float64(rowN) {
				blank++
			}
			if float64(rowInk) >= 0.6*float64(rowN) {
				rule++
			}
		}
	}
	if n == 0 {
		return PixelStats{}
	}
	ps := PixelStats{Ink: float64(ink) / float64(n), RuleRows: rule}
	if b.Dy() > 0 {
		ps.Aspect = float64(b.Dx()) / float64(b.Dy())
	}
	if ink > 0 {
		ps.Midtone = float64(mid) / float64(ink)
		ps.Sat = satSum / float64(ink)
		ps.Thin = float64(thin) / float64(ink)
		ps.Light = float64(light) / float64(ink)
		ps.Dark = float64(dark) / float64(ink)
	}
	if rows > 0 {
		ps.BlankRows = float64(blank) / float64(rows)
	}
	return ps
}
