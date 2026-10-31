// logotool derives favicon/app icons from static/logo.png: it finds the drop mark (the
// non-white pixels in the left square of the wordmark), crops it and writes PNGs at 32, 64,
// 180 and 512 px with a transparent background.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	src := "internal/web/static/logo.png"
	f, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		panic(err)
	}
	b := img.Bounds()
	h := b.Dy()
	// the mark occupies roughly the first square; find its non-white bbox
	minx, miny, maxx, maxy := h, h, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < h+10 && x < b.Dx(); x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < 240 || g>>8 < 240 || bl>>8 < 240 {
				if x < minx {
					minx = x
				}
				if y < miny {
					miny = y
				}
				if x > maxx {
					maxx = x
				}
				if y > maxy {
					maxy = y
				}
			}
		}
	}
	// stop at the first vertical gap after the mark (text starts after a white column run)
	gap := -1
	for x := minx; x < b.Dx(); x++ {
		white := true
		for y := miny; y <= maxy; y++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < 240 || g>>8 < 240 || bl>>8 < 240 {
				white = false
				break
			}
		}
		if white && x > minx+10 {
			gap = x
			break
		}
	}
	if gap > 0 {
		maxx = gap - 1
	}
	side := max(maxx-minx, maxy-miny) + 1
	pad := side / 10
	cx, cy := (minx+maxx)/2, (miny+maxy)/2
	square := image.Rect(cx-side/2-pad, cy-side/2-pad, cx+side/2+pad, cy+side/2+pad)
	fmt.Printf("mark bbox %d,%d-%d,%d → square %v\n", minx, miny, maxx, maxy, square)

	// transparent copy: white → alpha 0
	mark := image.NewNRGBA(image.Rect(0, 0, square.Dx(), square.Dy()))
	for y := 0; y < square.Dy(); y++ {
		for x := 0; x < square.Dx(); x++ {
			sx, sy := square.Min.X+x, square.Min.Y+y
			var c color.NRGBA
			if image.Pt(sx, sy).In(b) {
				r, g, bl, _ := img.At(sx, sy).RGBA()
				c = color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}
				if c.R >= 245 && c.G >= 245 && c.B >= 245 {
					c.A = 0
				}
			}
			mark.SetNRGBA(x, y, c)
		}
	}
	for _, size := range []int{32, 64, 180, 512} {
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), mark, mark.Bounds(), draw.Over, nil)
		out := fmt.Sprintf("internal/web/static/icon-%d.png", size)
		w, err := os.Create(out)
		if err != nil {
			panic(err)
		}
		if err := png.Encode(w, dst); err != nil {
			panic(err)
		}
		w.Close()
		fmt.Println("wrote", out)
	}
	// wordmark with transparent background for the header
	wm := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			c := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}
			if c.R >= 245 && c.G >= 245 && c.B >= 245 {
				c.A = 0
			}
			wm.SetNRGBA(x, y, c)
		}
	}
	w, _ := os.Create("internal/web/static/logo-transparent.png")
	png.Encode(w, wm)
	w.Close()
	fmt.Println("wrote internal/web/static/logo-transparent.png")
}
