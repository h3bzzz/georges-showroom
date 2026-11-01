package views

import (
	"net/url"
	"strconv"
	"strings"

	"georges_/internal/store"
)

// ShopURL builds sidebar links from the current filter state.
type ShopURL struct{ F store.BrowseFilter }

func (u ShopURL) base() string {
	if u.F.Room != "" {
		return "/shop/" + u.F.Room
	}
	return "/shop"
}

func (u ShopURL) values(f store.BrowseFilter) url.Values {
	v := url.Values{}
	if f.Category != "" {
		v.Set("category", f.Category)
	}
	for _, b := range f.Brands {
		v.Add("brand", b)
	}
	for _, x := range f.Finishes {
		v.Add("finish", x)
	}
	if f.MinCents > 0 {
		v.Set("min", strconv.FormatInt(f.MinCents/100, 10))
	}
	if f.MaxCents > 0 {
		v.Set("max", strconv.FormatInt(f.MaxCents/100, 10))
	}
	if f.Query != "" {
		v.Set("q", f.Query)
	}
	if f.Exact {
		v.Set("exact", "1")
	}
	keys := make([]string, 0, len(f.Specs))
	for k := range f.Specs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, x := range f.Specs[k] {
			v.Add("spec", k+":"+x)
		}
	}
	if f.Sort != "" && f.Sort != "relevance" {
		v.Set("sort", f.Sort)
	}
	if f.Page > 1 {
		v.Set("page", strconv.Itoa(f.Page))
	}
	return v
}

func (u ShopURL) build(f store.BrowseFilter) string {
	q := u.values(f).Encode()
	if q == "" {
		return u.base()
	}
	return u.base() + "?" + q
}

func (u ShopURL) Current() string { return u.build(u.F) }
func (u ShopURL) Page(n int) string {
	f := u.F
	f.Page = n
	return u.build(f)
}
func (u ShopURL) Category(c string) string {
	f := u.F
	f.Page = 0
	if f.Category == c {
		f.Category = ""
	} else {
		f.Category = c
	}
	return u.build(f)
}
func (u ShopURL) ToggleBrand(slug string) string {
	f := u.F
	f.Page = 0
	f.Brands = toggle(f.Brands, slug)
	return u.build(f)
}
func (u ShopURL) ToggleFinish(x string) string {
	f := u.F
	f.Page = 0
	f.Finishes = toggle(f.Finishes, x)
	return u.build(f)
}

// ToggleSpec adds or removes one attribute value ("Mounting" → "Wall Mount").
func (u ShopURL) ToggleSpec(k, x string) string {
	f := u.F
	f.Page = 0
	specs := make(map[string][]string, len(f.Specs))
	for kk, vs := range f.Specs {
		specs[kk] = append([]string(nil), vs...)
	}
	specs[k] = toggle(specs[k], x)
	if len(specs[k]) == 0 {
		delete(specs, k)
	}
	f.Specs = specs
	return u.build(f)
}
func (u ShopURL) Sort(s string) string {
	f := u.F
	f.Page = 0
	f.Sort = s
	return u.build(f)
}
func (u ShopURL) ClearPrice() string {
	f := u.F
	f.Page = 0
	f.MinCents, f.MaxCents = 0, 0
	return u.build(f)
}
func (u ShopURL) ClearQuery() string {
	f := u.F
	f.Page = 0
	f.Query = ""
	return u.build(f)
}
func (u ShopURL) ClearAll() string { return u.base() }

// AsTyped searches the original words with no spelling correction.
func (u ShopURL) AsTyped(q string) string {
	f := u.F
	f.Page = 0
	f.Query = q
	f.Exact = true
	return u.build(f)
}

// HiddenFields returns name/value pairs to keep the state inside the price form.
func (u ShopURL) HiddenFields() [][2]string {
	var out [][2]string
	v := u.values(u.F)
	for k, vs := range v {
		if k == "min" || k == "max" || k == "page" {
			continue
		}
		for _, x := range vs {
			out = append(out, [2]string{k, x})
		}
	}
	return out
}

func toggle(xs []string, v string) []string {
	var out []string
	found := false
	for _, x := range xs {
		if x == v {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		out = append(out, v)
	}
	return out
}

func RoomName(slug string) string {
	for _, r := range store.Rooms {
		if r.Slug == slug {
			return r.Name
		}
	}
	return "All products"
}

func Dollars(c int64) string {
	if c <= 0 {
		return ""
	}
	return strconv.FormatInt(c/100, 10)
}

func JoinLabels(xs []string) string { return strings.Join(xs, ", ") }

// QEsc escapes a query-string value.
func QEsc(s string) string { return url.QueryEscape(s) }
