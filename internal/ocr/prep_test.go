package ocr

import (
	"image"
	"image/color"
	"testing"
)

// A pale page (ink at grey 200 on paper 240) must come out black on white and twice as large.
func TestEnhance(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 40, 20))
	for i := range src.Pix {
		src.Pix[i] = 240
	}
	for x := 10; x < 30; x++ {
		src.SetGray(x, 10, color.Gray{200})
	}
	out, scale := Enhance(src)
	if scale != 2 || out.Bounds().Dx() != 80 || out.Bounds().Dy() != 40 {
		t.Fatalf("scale %v bounds %v", scale, out.Bounds())
	}
	if ink := out.GrayAt(40, 20).Y; ink > 60 {
		t.Errorf("ink stayed pale: %d", ink)
	}
	if paper := out.GrayAt(2, 2).Y; paper < 200 {
		t.Errorf("paper went dark: %d", paper)
	}
}
