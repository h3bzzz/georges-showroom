package assets

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// Measure is everything the picker and the page need to know about a picture, computed from a
// small working copy so a 2000 px master costs the same as a 200 px crop.
type Measure struct {
	Width, Height int             // canonical (trimmed) size in source pixels
	Trim          image.Rectangle // content box inside the source, in source pixels
	BgWhite       float32         // share of border pixels that are near-white (0..1)
	Color         string          // mean border colour, #rrggbb
	Fill          float32         // share of the trimmed frame the object occupies (0..1)
	Sharpness     float32         // Laplacian variance, log-scaled to 0..1
	LQIP          int             // Lean LQIP placeholder integer (see lqip.go)
	Cut           float32         // share of the busiest frame edge covered by the object (0..1)
}

const workSide = 256 // longest side of the working copy

// Analyze measures img. It never modifies img.
func Analyze(img image.Image) Measure {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var m Measure
	if w == 0 || h == 0 {
		return m
	}
	small := shrink(img, workSide)
	sb := small.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := float64(w) / float64(sw)

	// border ring: the picture's background, whatever colour it is
	var rs, gs, bs, n, white float64
	ring := max(1, sw/64)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			if x >= ring && x < sw-ring && y >= ring && y < sh-ring {
				continue
			}
			r, g, bl := rgb(small, x, y)
			rs, gs, bs, n = rs+r, gs+g, bs+bl, n+1
			if r > 235 && g > 235 && bl > 235 {
				white++
			}
		}
	}
	if n > 0 {
		rs, gs, bs = rs/n, gs/n, bs/n
		m.BgWhite = float32(white / n)
	}
	m.Color = fmt.Sprintf("#%02x%02x%02x", int(rs+0.5), int(gs+0.5), int(bs+0.5))

	// content box: pixels that differ from the background colour. Only trusted when the
	// border really is uniform; a busy catalog crop keeps its full frame.
	box := image.Rect(0, 0, sw, sh)
	if m.BgWhite > 0.85 || uniformBorder(small, rs, gs, bs) {
		box = contentBox(small, rs, gs, bs, 28)
	}
	if box.Empty() || box.Dx() < 4 || box.Dy() < 4 {
		box = image.Rect(0, 0, sw, sh)
	}
	// a little air around the object: 3% of the longer side, like a studio pad
	pad := max(1, int(0.03*float64(max(box.Dx(), box.Dy()))))
	box = box.Inset(-pad).Intersect(image.Rect(0, 0, sw, sh))
	m.Trim = image.Rect(int(float64(box.Min.X)*scale), int(float64(box.Min.Y)*scale),
		int(math.Ceil(float64(box.Max.X)*scale)), int(math.Ceil(float64(box.Max.Y)*scale))).Intersect(b.Sub(b.Min))
	m.Trim = m.Trim.Add(b.Min)
	m.Width, m.Height = m.Trim.Dx(), m.Trim.Dy()
	if m.Width == 0 || m.Height == 0 {
		m.Trim, m.Width, m.Height = b, w, h
	}
	// fill: how much of the trimmed frame is object, not background
	m.Fill = float32(objectShare(small, box, rs, gs, bs, 28))
	m.Cut = float32(edgeCut(small, rs, gs, bs, 60))
	m.Sharpness = sharpness(small)
	m.LQIP = LQIP(small)
	return m
}

// shrink returns img scaled so its longer side is side (or img itself when smaller).
func shrink(img image.Image, side int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		if r, ok := img.(*image.RGBA); ok && b.Min == (image.Point{}) {
			return r
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
		return dst
	}
	if w >= h {
		h = max(1, h*side/w)
		w = side
	} else {
		w = max(1, w*side/h)
		h = side
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func rgb(img *image.RGBA, x, y int) (float64, float64, float64) {
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+4 : i+4]
	// composite over white: transparent PNG masters read as white backgrounds
	a := float64(p[3]) / 255
	return float64(p[0])*a + 255*(1-a), float64(p[1])*a + 255*(1-a), float64(p[2])*a + 255*(1-a)
}

func uniformBorder(img *image.RGBA, r0, g0, b0 float64) bool {
	b := img.Bounds()
	var far, n float64
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if x > 0 && x < b.Dx()-1 && y > 0 && y < b.Dy()-1 {
				continue
			}
			r, g, bl := rgb(img, x, y)
			if math.Abs(r-r0)+math.Abs(g-g0)+math.Abs(bl-b0) > 60 {
				far++
			}
			n++
		}
	}
	return n > 0 && far/n < 0.08
}

// contentBox is the bounding box of pixels farther than tol (sum of channel deltas) from the
// background colour; rows/columns with fewer than 0.5% such pixels are treated as empty so
// JPEG ringing does not stretch the box.
func contentBox(img *image.RGBA, r0, g0, b0 float64, tol float64) image.Rectangle {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rows := make([]int, h)
	cols := make([]int, w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl := rgb(img, x, y)
			if math.Abs(r-r0)+math.Abs(g-g0)+math.Abs(bl-b0) > tol {
				rows[y]++
				cols[x]++
			}
		}
	}
	minRow := max(1, w/200)
	minCol := max(1, h/200)
	top, bottom, left, right := -1, -1, -1, -1
	for y := 0; y < h; y++ {
		if rows[y] >= minRow {
			if top < 0 {
				top = y
			}
			bottom = y
		}
	}
	for x := 0; x < w; x++ {
		if cols[x] >= minCol {
			if left < 0 {
				left = x
			}
			right = x
		}
	}
	if top < 0 || left < 0 {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right+1, bottom+1)
}

// edgeCut measures truncation: the share of pixels along each outer edge (a 2 px band) that
// belong to the object, taking the worst edge. A whole product on a studio background scores
// ~0; a faucet sliced by a column rule scores 0.3–0.9 on the sliced side. Thin bands of
// background noise are ignored by the tolerance.
func edgeCut(img *image.RGBA, r0, g0, b0, tol float64) float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 12 || h < 12 {
		return 0
	}
	// the outermost two pixels often carry a frame line or JPEG ringing, so the band sits
	// just inside them; tol is loose enough that a faint off-white border is not an object
	const skip, band = 2, 2
	edge := func(x0, y0, x1, y1 int) float64 {
		var obj, n float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				r, g, bl := rgb(img, x, y)
				if math.Abs(r-r0)+math.Abs(g-g0)+math.Abs(bl-b0) > tol {
					obj++
				}
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return obj / n
	}
	worst := edge(skip, skip, w-skip, skip+band)                     // top
	worst = math.Max(worst, edge(skip, h-skip-band, w-skip, h-skip)) // bottom
	worst = math.Max(worst, edge(skip, skip, skip+band, h-skip))     // left
	worst = math.Max(worst, edge(w-skip-band, skip, w-skip, h-skip)) // right
	return worst
}

func objectShare(img *image.RGBA, box image.Rectangle, r0, g0, b0, tol float64) float64 {
	var obj, n float64
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			r, g, bl := rgb(img, x, y)
			if math.Abs(r-r0)+math.Abs(g-g0)+math.Abs(bl-b0) > tol {
				obj++
			}
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return obj / n
}

// sharpness is the variance of a 3x3 Laplacian over the grey working copy, mapped with
// log10 onto 0..1 (variance 1 → 0, 10⁴ → 1). Blurry upscales score low, crisp masters high.
func sharpness(img *image.RGBA) float32 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return 0
	}
	gray := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl := rgb(img, x, y)
			gray[y*w+x] = 0.299*r + 0.587*g + 0.114*bl
		}
	}
	var sum, sum2, n float64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			l := 4*gray[y*w+x] - gray[y*w+x-1] - gray[y*w+x+1] - gray[(y-1)*w+x] - gray[(y+1)*w+x]
			sum += l
			sum2 += l * l
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / n
	variance := sum2/n - mean*mean
	if variance < 1 {
		return 0
	}
	return float32(math.Min(1, math.Log10(variance)/4))
}

// Quality folds the measures and the vision verdict into one 0..1 score the picker ranks by.
// Resolution and sharpness dominate; a white studio background and a well-filled frame add;
// the vision model's confidence that it is a whole-product photograph gates the rest.
func Quality(m Measure, kind string, kindScore float32) float32 {
	res := math.Min(1, math.Log2(float64(max(m.Width, m.Height))/80)/math.Log2(2000.0/80))
	if res < 0 {
		res = 0
	}
	q := 0.45*res + 0.30*float64(m.Sharpness) + 0.15*float64(m.BgWhite) + 0.10*fillScore(float64(m.Fill))
	switch kind {
	case "photo":
		q *= 0.6 + 0.4*float64(kindScore)
	case "", "detail":
		q *= 0.6
	case "drawing", "spec", "callout":
		q *= 0.5
	default: // text, lifestyle, other
		q *= 0.3
	}
	return float32(math.Max(0, math.Min(1, q)))
}

// a frame that is 35–85% object reads as a product shot; slivers and wall-to-wall scenes do not
func fillScore(f float64) float64 {
	switch {
	case f < 0.05:
		return 0
	case f < 0.35:
		return f / 0.35
	case f <= 0.85:
		return 1
	default:
		return math.Max(0, 1-(f-0.85)/0.15)
	}
}

var _ = color.RGBA{}
