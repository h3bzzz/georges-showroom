// Package assets is the imaging core. Every picture the site can show is an immutable,
// content-addressed asset: hashed once, measured once (sharpness, background, fill,
// placeholder, perceptual hashes), and rendered once into a fixed ladder of WebP widths that a
// static file server hands out with immutable cache headers. The web layer never resizes.
package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// LadderVersion changes whenever Widths, the trim rule or the encoder settings change; assets
// whose built column is older are rebuilt.
const LadderVersion = 1

// Widths is the rendition ladder: card tiles at 1x/2x (a 300 px tile), the product gallery and
// the hero/lightbox. Four files per asset keeps 100k assets under half a million files.
var Widths = []int{320, 640, 960, 1600}

// Quality is the lossy WebP quality; product silos on white compress very well at 82.
const WebPQuality = 82

// Ext is the rendition file extension.
const Ext = ".webp"

// ShortLen is how many hex characters of the hash a URL carries (64 bits).
const ShortLen = 16

// Short returns the URL form of a hash.
func Short(hash string) string {
	if len(hash) > ShortLen {
		return hash[:ShortLen]
	}
	return hash
}

// Dir is the rendition directory of a hash, relative to DATA_DIR: assets/ab/cd/<short>.
func Dir(hash string) string {
	s := Short(hash)
	return path.Join("assets", s[:2], s[2:4], s)
}

// RenditionPath is the relative path of one rendition: assets/ab/cd/<short>/640.webp.
func RenditionPath(hash string, w int) string {
	return path.Join(Dir(hash), strconv.Itoa(w)+Ext)
}

// URL is the public path of a rendition: /a/<short>/640.webp.
func URL(hash string, w int) string {
	return "/a/" + Short(hash) + "/" + strconv.Itoa(w) + Ext
}

// ParseURL reads /a/<short>/<w>.webp back into (short, w). ok is false for anything else,
// including widths outside the ladder, so a crawler cannot ask for arbitrary sizes.
func ParseURL(p string) (short string, w int, ok bool) {
	rest, found := strings.CutPrefix(p, "/a/")
	if !found {
		return "", 0, false
	}
	short, file, found := strings.Cut(rest, "/")
	if !found || len(short) != ShortLen || !isHex(short) {
		return "", 0, false
	}
	ws, found := strings.CutSuffix(file, Ext)
	if !found {
		return "", 0, false
	}
	w, err := strconv.Atoi(ws)
	if err != nil || !Allowed(w) {
		return "", 0, false
	}
	return short, w, true
}

// Allowed reports whether w is on the ladder.
func Allowed(w int) bool {
	for _, x := range Widths {
		if x == w {
			return true
		}
	}
	return false
}

// Pick returns the ladder width to request for a slot rendered at css pixels wide on a screen
// with the given device pixel ratio: the smallest rendition that is not smaller than needed.
func Pick(cssPx int, dpr float64) int {
	need := int(float64(cssPx)*dpr + 0.5)
	for _, w := range Widths {
		if w >= need {
			return w
		}
	}
	return Widths[len(Widths)-1]
}

// Srcset lists every ladder width no wider than the asset's own width (plus the first that
// exceeds it, so a 400 px source still offers 640 as its only upscaled fallback is never
// produced: renditions never upscale, the file is simply the source size).
func Srcset(hash string, assetW int) string {
	var b strings.Builder
	for _, w := range Widths {
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %dw", URL(hash, w), min(w, assetW))
		if w >= assetW {
			break
		}
	}
	return b.String()
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// HashFile returns the sha256 hex of a file's bytes and its size.
func HashFile(p string) (hash string, size int64, err error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Abs joins DATA_DIR and a relative path safely (no escaping the root).
func Abs(dataDir, rel string) (string, error) {
	clean := filepath.Clean("/" + filepath.FromSlash(rel))
	if clean == "/" {
		return "", fmt.Errorf("assets: empty path")
	}
	return filepath.Join(dataDir, clean), nil
}
