package site

import "testing"

func TestFixURL(t *testing.T) {
	in := "https://www.georgesshowroom.comhttps://www.georgesshowroom.com/Pasadena-California/X-1.HTM"
	if got := FixURL(in); got != "https://www.georgesshowroom.com/Pasadena-California/X-1.HTM" {
		t.Errorf("FixURL = %q", got)
	}
	if got := FixURL(" https://www.georgesshowroom.com/a.htm "); got != "https://www.georgesshowroom.com/a.htm" {
		t.Errorf("FixURL trim = %q", got)
	}
}

func TestBrandMatcher(t *testing.T) {
	m := NewBrandMatcher([]string{"Barclay", "Barclay-collections", "Native-trails", "Newport-brass", "Top-knobs"})
	cases := []struct{ u, slug, rest string }{
		{"https://www.georgesshowroom.com/Pasadena-California/Native-Trails-NSL3619-CX-Charcoal-Drop-In-Bathroom-Sink.HTM", "Native-trails", "NSL3619-CX-Charcoal-Drop-In-Bathroom-Sink"},
		{"https://www.georgesshowroom.com/Pasadena-California/Barclay-Collections-ABC-1-Tub.htm", "Barclay-collections", "ABC-1-Tub"},
		{"https://www.georgesshowroom.com/Pasadena-California/Barclay-ABC-1-Tub.HTM?x=1", "Barclay", "ABC-1-Tub"},
	}
	for _, c := range cases {
		slug, rest, ok := m.Match(c.u)
		if !ok || slug != c.slug || rest != c.rest {
			t.Errorf("%s → %q %q %v", c.u, slug, rest, ok)
		}
	}
	if _, _, ok := m.Match("https://www.georgesshowroom.com/Pasadena-California/Unknown-1.HTM"); ok {
		t.Error("unknown brand matched")
	}
	if _, _, ok := m.Match("https://www.georgesshowroom.com/Brands.HTML"); ok {
		t.Error("non-product matched")
	}
	if !IsProductURL(cases[0].u) || IsProductURL("https://www.georgesshowroom.com/NAT-X.JPEG") {
		t.Error("IsProductURL")
	}
}
