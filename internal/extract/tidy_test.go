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

// A hairline frame around the photo joins the caption printed inside it to the product; the
// frame is peeled and the caption above the product falls away.
func TestTidyFramedCaption(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 260))
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
	dark := color.RGBA{40, 40, 40, 255}
	gray := color.RGBA{120, 120, 120, 255}
	fill(image.Rect(0, 0, 200, 1), dark) // frame
	fill(image.Rect(0, 259, 200, 260), dark)
	fill(image.Rect(0, 0, 1, 260), dark)
	fill(image.Rect(199, 0, 200, 260), dark)
	fill(image.Rect(20, 6, 150, 22), dark)   // caption inside the frame, above the product
	fill(image.Rect(60, 60, 140, 240), gray) // the product
	keep, ok := Tidy(img)
	if !ok {
		t.Fatal("expected a trim")
	}
	if keep.Min.Y < 40 || keep.Min.Y > 60 || keep.Max.Y < 240 || keep.Min.X > 60 || keep.Max.X < 140 {
		t.Errorf("kept %v, want the product below the caption", keep)
	}
}

// A caption on the very first rows, one blank row above the product, is cut on a tight crop.
func TestTidyTightCaption(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 127, 167))
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
	dark := color.RGBA{40, 40, 40, 255}
	gray := color.RGBA{120, 120, 120, 255}
	fill(image.Rect(10, 0, 100, 7), dark)  // "…ed Handle", cut by the crop edge
	fill(image.Rect(30, 8, 95, 167), gray) // the faucet, one blank row below the caption
	keep, ok := Tidy(img)
	if !ok {
		t.Fatal("expected a trim")
	}
	if keep.Min.Y < 6 || keep.Min.Y > 9 {
		t.Errorf("kept %v, want the caption rows cut", keep)
	}
}
