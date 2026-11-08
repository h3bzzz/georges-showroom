// Package views holds the templ templates and the small helpers they call.
package views

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"georges/internal/assets"
	"georges/internal/store"
)

// AssetVersion is the hash of app.css, set by the server for cache busting.
var AssetVersion = "0"

// Money formats cents as "$1,234" (whole dollars; the trade shows list prices rounded).
func Money(c int64) string {
	d := c / 100
	s := strconv.FormatInt(d, 10)
	if len(s) > 3 {
		var b strings.Builder
		pre := len(s) % 3
		if pre > 0 {
			b.WriteString(s[:pre])
		}
		for i := pre; i < len(s); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(s[i : i+3])
		}
		s = b.String()
	}
	if c%100 != 0 {
		return fmt.Sprintf("$%s.%02d", s, c%100)
	}
	return "$" + s
}

// Price is the card price line.
func Price(c *int64) string {
	if c == nil || *c <= 0 {
		return ""
	}
	return Money(*c)
}

// CardImg are the attributes of a card tile image.
func CardImg(hash string, w, h int) templ.Attributes {
	a := templ.Attributes{
		"src":      assets.URL(hash, 320),
		"srcset":   assets.Srcset(hash, max(w, 320)),
		"sizes":    "(min-width:1280px) 280px, (min-width:768px) 30vw, 46vw",
		"loading":  "lazy",
		"decoding": "async",
	}
	if w > 0 && h > 0 {
		a["width"] = strconv.Itoa(w)
		a["height"] = strconv.Itoa(h)
	}
	return a
}

// Eager marks the first tiles so the browser fetches them before the fold's lazy ones.
func Eager(a templ.Attributes, i int) templ.Attributes {
	if i < 4 {
		a["loading"] = "eager"
		if i == 0 {
			a["fetchpriority"] = "high"
		}
	}
	return a
}

// Placeholder is the inline style carrying the Lean LQIP integer the stylesheet decodes.
func Placeholder(l *int) templ.Attributes {
	if l == nil || *l == 0 {
		return templ.Attributes{}
	}
	return templ.Attributes{"style": "--lqip:" + strconv.Itoa(*l)}
}

// GalleryImg are the attributes for the main product image at the given slot width.
func GalleryImg(hash string, w, h int, eager bool) templ.Attributes {
	a := templ.Attributes{
		"src":      assets.URL(hash, 960),
		"srcset":   assets.Srcset(hash, max(w, 960)),
		"sizes":    "(min-width:1024px) 640px, 100vw",
		"decoding": "async",
	}
	if eager {
		a["fetchpriority"] = "high"
	} else {
		a["loading"] = "lazy"
	}
	if w > 0 && h > 0 {
		a["width"] = strconv.Itoa(w)
		a["height"] = strconv.Itoa(h)
	}
	return a
}

// Thumb is a small rendition URL.
func Thumb(hash string) string { return assets.URL(hash, 320) }

// Full is the lightbox rendition URL.
func Full(hash string) string { return assets.URL(hash, 1600) }

// ShopURL builds browse links that keep the current filter and change one part.
type ShopURL struct {
	Filter store.Filter
}

func (u ShopURL) base() (string, url.Values) {
	p := "/shop"
	if u.Filter.Room != "" {
		p += "/" + u.Filter.Room
	}
	v := url.Values{}
	if u.Filter.Category != "" {
		v.Set("category", u.Filter.Category)
	}
	if u.Filter.Brand != "" {
		v.Set("brand", u.Filter.Brand)
	}
	for _, t := range u.Filter.Tones {
		v.Add("tone", t)
	}
	if u.Filter.Query != "" {
		v.Set("q", u.Filter.Query)
	}
	if u.Filter.Sort != "" {
		v.Set("sort", u.Filter.Sort)
	}
	if u.Filter.PriceMin > 0 {
		v.Set("min", strconv.FormatInt(u.Filter.PriceMin/100, 10))
	}
	if u.Filter.PriceMax > 0 {
		v.Set("max", strconv.FormatInt(u.Filter.PriceMax/100, 10))
	}
	if u.Filter.ShowParts {
		v.Set("parts", "1")
	}
	return p, v
}

func enc(p string, v url.Values) string {
	if len(v) == 0 {
		return p
	}
	return p + "?" + v.Encode()
}

// Page links to page n.
func (u ShopURL) Page(n int) string {
	p, v := u.base()
	if n > 1 {
		v.Set("page", strconv.Itoa(n))
	}
	return enc(p, v)
}

// Category sets or clears the category.
func (u ShopURL) Category(c string) string {
	p, v := u.base()
	v.Del("category")
	if c != "" && c != u.Filter.Category {
		v.Set("category", c)
	}
	return enc(p, v)
}

// Brand sets or clears the brand.
func (u ShopURL) Brand(b string) string {
	p, v := u.base()
	v.Del("brand")
	if b != "" && b != u.Filter.Brand {
		v.Set("brand", b)
	}
	return enc(p, v)
}

// Tone toggles a tone.
func (u ShopURL) Tone(t string) string {
	p, v := u.base()
	v.Del("tone")
	on := false
	for _, x := range u.Filter.Tones {
		if x == t {
			on = true
			continue
		}
		v.Add("tone", x)
	}
	if !on {
		v.Add("tone", t)
	}
	return enc(p, v)
}

// HasTone reports whether t is active.
func (u ShopURL) HasTone(t string) bool {
	for _, x := range u.Filter.Tones {
		if x == t {
			return true
		}
	}
	return false
}

// Sort sets the sort.
func (u ShopURL) Sort(s string) string {
	p, v := u.base()
	v.Del("sort")
	if s != "" {
		v.Set("sort", s)
	}
	return enc(p, v)
}

// Parts toggles parts visibility.
func (u ShopURL) Parts() string {
	p, v := u.base()
	if u.Filter.ShowParts {
		v.Del("parts")
	} else {
		v.Set("parts", "1")
	}
	return enc(p, v)
}

// Clear drops every filter but the room.
func (u ShopURL) Clear() string {
	p := "/shop"
	if u.Filter.Room != "" {
		p += "/" + u.Filter.Room
	}
	return p
}

// Active lists the active filter chips.
func (u ShopURL) Active() [][2]string {
	var out [][2]string
	if u.Filter.Query != "" {
		out = append(out, [2]string{"“" + u.Filter.Query + "”", u.without("q")})
	}
	if u.Filter.Category != "" {
		out = append(out, [2]string{u.Filter.Category, u.Category("")})
	}
	if u.Filter.Brand != "" {
		out = append(out, [2]string{"Brand: " + u.Filter.Brand, u.Brand("")})
	}
	for _, t := range u.Filter.Tones {
		out = append(out, [2]string{t, u.Tone(t)})
	}
	if u.Filter.PriceMin > 0 || u.Filter.PriceMax > 0 {
		out = append(out, [2]string{"Price", u.without("min", "max")})
	}
	return out
}

func (u ShopURL) without(keys ...string) string {
	p, v := u.base()
	for _, k := range keys {
		v.Del(k)
	}
	return enc(p, v)
}

// Itoa for templates.
func Itoa(i int) string { return strconv.Itoa(i) }

// Plural picks a noun form.
func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Thousands formats a count with separators.
func Thousands(n int) string {
	return strings.TrimPrefix(Money(int64(n)*100), "$")
}

// ToneColor is the swatch colour for a tone group.
func ToneColor(t string) string {
	switch t {
	case "silver":
		return "linear-gradient(135deg,#e9ecef,#9aa3ab)"
	case "gold":
		return "linear-gradient(135deg,#f3dba0,#b9892f)"
	case "bronze":
		return "linear-gradient(135deg,#8a5a3a,#3b2418)"
	case "black":
		return "linear-gradient(135deg,#3a3a3a,#0b0b0b)"
	case "white":
		return "linear-gradient(135deg,#ffffff,#dcdcdc)"
	case "copper":
		return "linear-gradient(135deg,#e2a07a,#9a4e2a)"
	}
	return "#ccc"
}

// SwatchStyle returns the inline style for a tone dot.
func SwatchStyle(t string) templ.Attributes {
	return templ.Attributes{"style": "background:" + ToneColor(t), "title": t}
}

// FileURL serves a DATA_DIR-relative file through the allow-listed /files route.
func FileURL(rel string) string { return "/files/" + strings.TrimPrefix(rel, "/") }

// PageImg is the catalog page image route.
func PageImg(catalogID, page int) string { return fmt.Sprintf("/catalog/%d/page/%d", catalogID, page) }

// Truncate shortens text for cards.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndex(s[:n], " ")
	if cut < n/2 {
		cut = n
	}
	return s[:cut] + "…"
}
