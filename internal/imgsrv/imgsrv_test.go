package imgsrv

import (
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"crops/GES/book/0001_00.jpg":    "crops/GES/book/0001_00.jpg",
		"/crops/GES/book/0001_00.JPG":   "crops/GES/book/0001_00.JPG",
		"crops/../pg/data/pg_hba.conf":  "",
		"pg/data/pg_hba.conf":           "",
		"pipeline.log":                  "",
		"crops":                         "",
		"crops/":                        "",
		"crops/GES/book/0001_00.jpg/..": "",
		"logos/kohler.svg":              "logos/kohler.svg",
		"catalogs/GES/book/0001.jpg":    "catalogs/GES/book/0001.jpg",
		"..\\..\\etc\\passwd":           "",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResizedAndOriginal(t *testing.T) {
	dir := t.TempDir()
	cropDir := filepath.Join(dir, "crops", "X", "book")
	if err := os.MkdirAll(cropDir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(10, 10, color.RGBA{255, 0, 0, 255})
	f, _ := os.Create(filepath.Join(cropDir, "a.jpg"))
	_ = jpeg.Encode(f, img, nil)
	f.Close()
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("no"), 0o644)

	s := New(dir)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		if len(p) > 5 && p[:5] == "/img/" {
			s.Resized(rec, req)
		} else {
			s.Original(rec, req)
		}
		return rec
	}
	if rec := get("/img/320/crops/X/book/a.jpg"); rec.Code != 200 {
		t.Fatalf("resized: %d %s", rec.Code, rec.Body.String())
	} else {
		cfg, err := jpeg.DecodeConfig(rec.Body)
		if err != nil || cfg.Width != 320 || cfg.Height != 160 {
			t.Fatalf("rendition size %+v err %v", cfg, err)
		}
		if cc := rec.Header().Get("Cache-Control"); cc == "" {
			t.Error("missing Cache-Control")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "img", "320", "crops", "X", "book", "a.jpg")); err != nil {
		t.Errorf("rendition not cached: %v", err)
	}
	// wider than the source: served as the original, nothing written
	if rec := get("/img/1280/crops/X/book/a.jpg"); rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("passthrough: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "img", "1280")); err == nil {
		t.Error("upscaled rendition should not be cached")
	}
	for _, bad := range []string{"/img/333/crops/X/book/a.jpg", "/img/320/secret.txt", "/files/secret.txt", "/files/", "/files/crops/", "/files/crops/X/book/"} {
		if rec := get(bad); rec.Code != 404 {
			t.Errorf("%s: want 404, got %d", bad, rec.Code)
		}
	}
	if rec := get("/files/crops/X/book/a.jpg"); rec.Code != 200 {
		t.Errorf("original: %d", rec.Code)
	}
}
