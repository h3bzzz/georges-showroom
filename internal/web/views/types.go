package views

import (
	"fmt"
	"strconv"
	"strings"

	"georges_/internal/store"
)

type Stats struct{ Brands, Families, Products, Catalogs, Pages int }

type Pager struct {
	Page, Total, PerPage int
	Base                 string
}

func (p Pager) Pages() int {
	if p.PerPage == 0 {
		return 1
	}
	n := (p.Total + p.PerPage - 1) / p.PerPage
	if n < 1 {
		n = 1
	}
	return n
}
func (p Pager) HasPrev() bool { return p.Page > 1 }
func (p Pager) HasNext() bool { return p.Page < p.Pages() }
func (p Pager) URL(n int) string {
	sep := "?"
	if len(p.Base) > 0 && (contains(p.Base, "?")) {
		sep = "&"
	}
	return fmt.Sprintf("%s%spage=%d", p.Base, sep, n)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Money renders cents as $1,234.56.
func Money(c int64) string {
	if c <= 0 {
		return ""
	}
	d := c / 100
	s := fmt.Sprintf("%d", d)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return fmt.Sprintf("$%s.%02d", s, c%100)
}

func PriceRange(min, max int64) string {
	switch {
	case min == 0 && max == 0:
		return "Call for price"
	case min == max:
		return Money(min)
	default:
		return Money(min) + " – " + Money(max)
	}
}

func Itoa(i int) string  { return fmt.Sprintf("%d", i) }
func I64(i int64) string { return fmt.Sprintf("%d", i) }

// RoomTile is a landing-page tile backed by a catalog cover.
type RoomTile struct {
	Room      string
	Href      string
	CatalogID int
	Page      int    // page used for large banners (1 = cover)
	Image     string // lifestyle crop URL when one exists (preferred over the page render)
	Caption   string
}

// Initial returns the first letter used to group brands.
func Initial(name string) string {
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			return string(r - 32)
		}
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return string(r)
		}
	}
	return "#"
}

func Thousands(i int) string {
	s := fmt.Sprintf("%d", i)
	for n := len(s) - 3; n > 0; n -= 3 {
		s = s[:n] + "," + s[n:]
	}
	return s
}

// SavePct returns the percent saved between list and ours (0 when unknown).
func SavePct(list, ours int64) int {
	if list <= 0 || ours <= 0 || ours >= list {
		return 0
	}
	return int((list - ours) * 100 / list)
}

// ImgStyle caps the displayed size of a small source image so it is never upscaled into blur.
func ImgStyle(w int) string {
	if w <= 0 || w >= 420 {
		return ""
	}
	return fmt.Sprintf("max-width:%dpx;max-height:%dpx", w*3/2, w*3/2)
}

// CoverSet pairs a category tile with candidate photographs for the curation page.
type CoverSet struct {
	Tile       store.CategoryTile
	Candidates []store.Cover
}

// AnchorFor gives a stable fragment id for a cover block.
func AnchorFor(kind, key string) string { return "c-" + store.Slugify(kind+"-"+key) }

// HeroBook is one catalog in the home page cover stack.
type HeroBook struct {
	ID    int
	Title string
	Image string
}

// Img turns a stored image URL ("/files/crops/…") into its w-wide rendition ("/img/480/crops/…").
// Other URLs (static icons, catalog page routes, remote) pass through unchanged.
func Img(src string, w int) string {
	rest, ok := strings.CutPrefix(src, "/files/")
	if !ok || rest == "" {
		return src
	}
	return "/img/" + strconv.Itoa(w) + "/" + rest
}

// Srcset lists renditions for the browser to pick from by layout width and device pixel ratio.
func Srcset(src string, widths ...int) string {
	if !strings.HasPrefix(src, "/files/") {
		return ""
	}
	parts := make([]string, 0, len(widths))
	for _, w := range widths {
		parts = append(parts, Img(src, w)+" "+strconv.Itoa(w)+"w")
	}
	return strings.Join(parts, ", ")
}

// Card renditions: a product card is 150–320 CSS px wide, so 320/640/960 covers 1x–3x screens.
const (
	CardSizes = "(min-width:1280px) 300px, (min-width:768px) 33vw, 50vw"
	HeroSizes = "(min-width:1024px) 50vw, 100vw"
)

func CardSrcset(src string) string { return Srcset(src, 320, 640, 960) }
func HeroSrcset(src string) string { return Srcset(src, 480, 960, 1280) }

// tileTitle is the hover text of a landing tile: what the click shows and where the photo is from.
func tileTitle(t store.HomeTileView) string {
	s := "Shop " + t.Name
	if t.Count > 0 {
		s += " · " + Thousands(t.Count) + " products"
	}
	if t.Caption != "" {
		s += " · photo from " + t.Caption
	}
	return s
}

// HomeCoverSet pairs a landing tile with candidate photographs for the curation page.
type HomeCoverSet struct {
	Tile       store.HomeTileView
	Candidates []store.Cover
}

// AssetVersion is appended to static asset URLs (?v=…) so the 24-hour Cache-Control on
// /static never serves a stale stylesheet after a deploy. The server sets it from the
// embedded file contents at start-up.
var AssetVersion = "dev"
