package upscale

import (
	"image"
	"os"

	"golang.org/x/image/draw"

	_ "image/jpeg"
	_ "image/png"
)

// Divergence measures how far an HD copy strays from the crop it was made from: both are
// reduced to the same small thumbnail and compared pixel by pixel (mean absolute difference
// over RGB, 0..255). A faithful 4x upscale lands under ~12; a copy the GPU garbled (banded
// noise, tiles in the wrong place — it happens with ncnn-vulkan on a hot or busy card) sits
// far above 30. Returns -1 when either file cannot be read.
func Divergence(nativePath, hdPath string) float64 {
	a := thumb(nativePath, 48)
	b := thumb(hdPath, 48)
	if a == nil || b == nil {
		return -1
	}
	var sum float64
	n := 0
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			ra, ga, ba, _ := a.At(x, y).RGBA()
			rb, gb, bb, _ := b.At(x, y).RGBA()
			sum += absDiff(ra, rb) + absDiff(ga, gb) + absDiff(ba, bb)
			n += 3
		}
	}
	return sum / float64(n) / 257
}

func absDiff(a, b uint32) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}

func thumb(path string, size int) *image.RGBA {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}
