package extract

import (
	"image"
	"image/color"
	"testing"
)

func TestTidy(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 240, 200))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	fill := func(r image.Rectangle, c color.RGBA) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	gray := color.RGBA{120, 120, 120, 255}
	fill(image.Rect(40, 20, 150, 150), gray)  // the product
	fill(image.Rect(232, 30, 240, 120), gray) // neighbour sliver at the right edge
	fill(image.Rect(60, 185, 110, 195), gray) // caption under the photo
	keep, ok := Tidy(img)
	if !ok {
		t.Fatal("expected a trim")
	}
	if keep.Max.X > 160 || keep.Max.Y > 160 || keep.Min.X > 40 || keep.Min.Y > 20 {
		t.Errorf("kept %v, want the product box with padding", keep)
	}
	// a two-piece product: both pieces heavy → no trim between them
	img2 := image.NewRGBA(image.Rect(0, 0, 240, 200))
	for i := range img2.Pix {
		img2.Pix[i] = 255
	}
	img = img2
	fill(image.Rect(10, 20, 100, 180), gray)
	fill(image.Rect(140, 20, 230, 180), gray)
	if keep, ok := Tidy(img); ok && (keep.Min.X > 10 || keep.Max.X < 230) {
		t.Errorf("two-piece product split: %v", keep)
	}
}
