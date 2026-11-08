package assets

import (
	"bufio"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Decode opens and decodes an image file.
func Decode(p string) (image.Image, string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(bufio.NewReaderSize(f, 1<<16))
	if err != nil {
		return nil, "", fmt.Errorf("decode %s: %w", filepath.Base(p), err)
	}
	return img, format, nil
}

// Build writes the whole rendition ladder for img (already measured) under dataDir and
// returns how many files it wrote. Renditions never upscale: a width above the trimmed
// source is written at the source size so every ladder URL resolves. Files are written
// atomically (tmp + rename) so a killed job never leaves a truncated WebP behind.
func Build(dataDir, hash string, img image.Image, m Measure) (int, error) {
	dir := filepath.Join(dataDir, filepath.FromSlash(Dir(hash)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	src := crop(img, m.Trim)
	wrote := 0
	var prev *image.RGBA
	for i := len(Widths) - 1; i >= 0; i-- { // largest first: each step shrinks the previous
		w := Widths[i]
		out := filepath.Join(dir, fmt.Sprintf("%d%s", w, Ext))
		tw := min(w, src.Bounds().Dx())
		th := max(1, src.Bounds().Dy()*tw/max(1, src.Bounds().Dx()))
		var from image.Image = src
		if prev != nil {
			from = prev
		}
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		if tw == src.Bounds().Dx() {
			draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
		} else {
			draw.CatmullRom.Scale(dst, dst.Bounds(), from, from.Bounds(), draw.Src, nil)
		}
		if err := WriteWebP(out, dst); err != nil {
			return wrote, err
		}
		prev = dst
		wrote++
	}
	return wrote, nil
}

// crop returns the sub-image r of img as a fresh RGBA with alpha composited over white.
func crop(img image.Image, r image.Rectangle) *image.RGBA {
	if r.Empty() {
		r = img.Bounds()
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Over)
	return dst
}

// WriteWebP encodes img to out atomically.
func WriteWebP(out string, img image.Image) error {
	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<16)
	if err := webp.Encode(bw, img, webp.Options{Quality: WebPQuality, Method: 4}); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}
