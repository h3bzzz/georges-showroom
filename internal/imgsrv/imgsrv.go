// Package imgsrv serves the site's derived images (catalog crops, covers, logos) and nothing
// else. It does one job: given an allow-listed image path and a width, hand back a cached,
// well-downsampled rendition with long cache headers. It never lists directories and never
// touches anything outside the allowed roots, so DATA_DIR can keep holding the Postgres data,
// price files and logs next to the images.
package imgsrv

import (
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decode only; logos pushed from browsers may be webp
	"golang.org/x/sync/singleflight"
)

// Roots are the DATA_DIR sub-directories that may be served.
var Roots = []string{"crops", "crops_hd", "covers", "logos", "webimg", "catalogs", "specimg"}

// Widths are the only renditions /img/{w}/… will produce (anything else is a 404), so a
// crawler cannot fill the disk with arbitrary sizes.
var Widths = []int{160, 320, 480, 640, 960, 1280}

const (
	cacheMaxAge = 7 * 24 * time.Hour
	jpegQuality = 85
)

var exts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".svg": true}

type Server struct {
	dataDir  string
	cacheDir string
	group    singleflight.Group
}

func New(dataDir string) *Server {
	return &Server{dataDir: dataDir, cacheDir: filepath.Join(dataDir, "cache", "img")}
}

// Clean validates a relative image path ("crops/GES/book/0012_00.jpg"). It returns "" when the
// path escapes the roots, is not an image, or is a directory.
func Clean(rel string) string {
	rel = path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))[1:]
	if rel == "" || strings.HasPrefix(rel, "..") || !exts[strings.ToLower(path.Ext(rel))] {
		return ""
	}
	root, _, ok := strings.Cut(rel, "/")
	if !ok {
		return ""
	}
	for _, r := range Roots {
		if root == r {
			return rel
		}
	}
	return ""
}

// Original serves the file as stored (mounted at /files/).
func (s *Server) Original(w http.ResponseWriter, r *http.Request) {
	rel := Clean(strings.TrimPrefix(r.URL.Path, "/files/"))
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	s.serveFile(w, r, filepath.Join(s.dataDir, filepath.FromSlash(rel)))
}

// Resized serves /img/{w}/{path}: the image downsampled to at most w pixels wide. Sources
// already narrower than w, and formats we do not resample (gif, svg), are served as-is.
func (s *Server) Resized(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/img/")
	ws, rel, ok := strings.Cut(rest, "/")
	width, err := strconv.Atoi(ws)
	if !ok || err != nil || !allowedWidth(width) {
		http.NotFound(w, r)
		return
	}
	rel = Clean(rel)
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	src := filepath.Join(s.dataDir, filepath.FromSlash(rel))
	ext := strings.ToLower(path.Ext(rel))
	if ext == ".gif" || ext == ".svg" {
		s.serveFile(w, r, src)
		return
	}
	out, err := s.rendition(src, rel, width)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	s.serveFile(w, r, out)
}

func allowedWidth(w int) bool {
	for _, x := range Widths {
		if x == w {
			return true
		}
	}
	return false
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, p string) {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(cacheMaxAge.Seconds())))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// rendition returns the path of the cached w-wide copy of src, building it when missing or
// older than the source (crops are rewritten in place by `catalogs extract`). Concurrent
// first requests for the same rendition share one resize.
func (s *Server) rendition(src, rel string, width int) (string, error) {
	sst, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	out := filepath.Join(s.cacheDir, strconv.Itoa(width), filepath.FromSlash(strings.TrimSuffix(rel, path.Ext(rel))+".jpg"))
	if ost, err := os.Stat(out); err == nil && !ost.ModTime().Before(sst.ModTime()) {
		return out, nil
	}
	v, err, _ := s.group.Do(out, func() (any, error) {
		p, err := resize(src, out, width)
		return p, err
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// resize writes src scaled to width into out (JPEG). When the source is not wider than width
// it returns src itself: never upscale, never re-encode what is already right.
func resize(src, out string, width int) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", src, err)
	}
	b := img.Bounds()
	if b.Dx() <= width {
		return src, nil
	}
	h := int(float64(b.Dy()) * float64(width) / float64(b.Dx()))
	if h < 1 {
		h = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, h))
	// png/gif sources (logos) may carry alpha: flatten onto white, which is the card background
	if format != "jpeg" {
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := jpeg.Encode(tmp, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return out, os.Rename(tmp.Name(), out)
}

// keep the decoders linked even if nothing else in this package names them
var (
	_ = png.Decode
	_ = gif.Decode
	_ io.Reader
)
