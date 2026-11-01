package ocr

import (
	"image"
	"image/draw"
	"image/png"
	_ "image/jpeg"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

// Enhance prepares a page whose type is too thin or too pale for tesseract (Newport Brass
// prints its price book in light grey hairline figures: 0 words raw, 220 after this):
// greyscale, a contrast stretch that puts the 1st percentile at black and the 99th at
// white, then a 2× upscale so hairlines are two pixels wide. Returns the scale applied.
func Enhance(src image.Image) (*image.Gray, float64) {
	b := src.Bounds()
	g := image.NewGray(b)
	draw.Draw(g, b, src, b.Min, draw.Src)
	var hist [256]int
	for _, v := range g.Pix {
		hist[v]++
	}
	n := len(g.Pix)
	pct := func(p float64) int {
		want := int(float64(n) * p)
		acc := 0
		for v := 0; v < 256; v++ {
			acc += hist[v]
			if acc >= want {
				return v
			}
		}
		return 255
	}
	lo, hi := pct(0.01), pct(0.99)
	if hi-lo >= 16 {
		var lut [256]uint8
		for v := range lut {
			x := (float64(v) - float64(lo)) * 255 / float64(hi-lo)
			lut[v] = uint8(min(255, max(0, x)))
		}
		for i, v := range g.Pix {
			g.Pix[i] = lut[v]
		}
	}
	const scale = 2
	out := image.NewGray(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	xdraw.CatmullRom.Scale(out, out.Bounds(), g, b, xdraw.Src, nil)
	return out, scale
}

// enhanceToTemp writes the enhanced page as a PNG beside the system temp dir; the caller removes it.
func enhanceToTemp(path string) (string, float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", 0, err
	}
	g, scale := Enhance(img)
	tmp, err := os.CreateTemp("", "georges-ocr-*.png")
	if err != nil {
		return "", 0, err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(tmp, g); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", 0, err
	}
	tmp.Close()
	return filepath.Clean(tmp.Name()), scale, nil
}

// confident counts words tesseract is at least half sure of.
func confident(ws []Word) int {
	n := 0
	for _, w := range ws {
		if w.Conf >= 50 {
			n++
		}
	}
	return n
}

