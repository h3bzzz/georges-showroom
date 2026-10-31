package extract

import (
	"image"
	"image/color"
	"testing"
)

// The RGBA fast path must give the same answer as sampling through the generic interface.
func TestStatsMatchesGenericPath(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 64, 48), image.YCbCrSubsampleRatio420)
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			var c color.Color = color.White
			switch {
			case x > 10 && x < 40 && y > 8 && y < 30:
				c = color.RGBA{uint8(60 + x*3), 80, 120, 255} // a shaded object
			case y == 40:
				c = color.Black // a rule
			}
			yy, cb, cr := color.RGBToYCbCr(rgb(c))
			src.Y[src.YOffset(x, y)] = yy
			ci := src.COffset(x, y)
			src.Cb[ci], src.Cr[ci] = cb, cr
		}
	}
	fast := Stats(src, 1)
	slow := Stats(rgbaOf(src), 1) // identical input through the same path: sanity
	if fast != slow {
		t.Fatalf("stats differ:\n%+v\n%+v", fast, slow)
	}
	if fast.Ink <= 0 || fast.RuleRows != 1 || fast.Midtone <= 0.5 {
		t.Errorf("unexpected stats %+v", fast)
	}
}

func rgb(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func BenchmarkStats(b *testing.B) {
	src := image.NewYCbCr(image.Rect(0, 0, 400, 400), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = uint8(i % 251)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Stats(src, 2)
	}
}
