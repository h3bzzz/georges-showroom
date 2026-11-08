package assets

import (
	"image"
	"image/color"
	"testing"
)

// a grey "faucet" rectangle on a white studio background, off-centre with wide margins
func silo(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	for y := h / 4; y < h*3/4; y++ {
		for x := w / 3; x < w/2; x++ {
			img.Set(x, y, color.RGBA{90, 95, 100, 255})
		}
	}
	return img
}

func TestAnalyzeTrimsWhiteMargins(t *testing.T) {
	m := Analyze(silo(600, 400))
	if m.BgWhite < 0.95 {
		t.Fatalf("bg_white = %v, want ~1", m.BgWhite)
	}
	if m.Trim.Min.X < 180 || m.Trim.Min.X > 200 || m.Trim.Max.X < 300 || m.Trim.Max.X > 320 {
		t.Fatalf("trim x = %v, want ≈[200,300] with a small pad", m.Trim)
	}
	if m.Fill < 0.6 {
		t.Fatalf("fill = %v, want most of the trimmed frame", m.Fill)
	}
	if m.Color != "#ffffff" {
		t.Fatalf("color = %s", m.Color)
	}
}

func TestCutDetectsTruncation(t *testing.T) {
	whole := Analyze(silo(600, 400))
	if whole.Cut > 0.05 {
		t.Fatalf("whole object cut = %v", whole.Cut)
	}
	// the same object sliced through the middle: the right edge is all object
	img := silo(600, 400)
	half := img.SubImage(image.Rect(0, 0, 250, 400)).(*image.RGBA)
	cut := Analyze(half)
	if cut.Cut < 0.3 {
		t.Fatalf("sliced object cut = %v, want ≥ 0.3", cut.Cut)
	}
}

func TestAnalyzeKeepsBusyFrame(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	m := Analyze(img)
	if m.Trim != img.Bounds() {
		t.Fatalf("busy picture should keep its frame, got %v", m.Trim)
	}
	if m.Sharpness <= 0 {
		t.Fatalf("sharpness = %v", m.Sharpness)
	}
}

func TestLQIPRange(t *testing.T) {
	v := LQIP(silo(300, 300))
	if v <= 0 || v >= 1<<20 {
		t.Fatalf("lqip %d out of 20 bits", v)
	}
	// white picture: high L, neutral a/b
	if l := (v >> 6) & 3; l < 2 {
		t.Fatalf("base luminance %d, want bright", l)
	}
}

func TestURLRoundTrip(t *testing.T) {
	h := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	u := URL(h, 640)
	if u != "/a/0123456789abcdef/640.webp" {
		t.Fatal(u)
	}
	s, w, ok := ParseURL(u)
	if !ok || s != Short(h) || w != 640 {
		t.Fatal(s, w, ok)
	}
	if _, _, ok := ParseURL("/a/0123456789abcdef/500.webp"); ok {
		t.Fatal("off-ladder width accepted")
	}
	if Pick(300, 2) != 640 || Pick(300, 1) != 320 || Pick(1000, 2) != 1600 {
		t.Fatal("pick")
	}
}

func TestPerceptualStable(t *testing.T) {
	a := silo(400, 300)
	_, pa, err := Perceptual(a)
	if err != nil {
		t.Fatal(err)
	}
	_, pb, _ := Perceptual(shrink(a, 200))
	if !PDQSame(pa, pb) {
		t.Fatalf("same picture at two sizes should match: distance %d", Hamming(pa, pb))
	}
}
