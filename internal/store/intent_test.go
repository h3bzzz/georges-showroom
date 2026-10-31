package store

import "testing"

func TestParseIntentBrandSpacing(t *testing.T) {
	brands := map[string]string{"lacava": "Lacava", "top knobs": "Top-knobs", "sub-zero": "Sub-zero", "subzero": "Sub-zero", "native trails": "Native-trails"}
	cases := []struct {
		q     string
		slug  string
		words int // concepts left after the brand is removed
	}{
		{"la cava trough sink", "Lacava", 2},
		{"lacava sinks", "Lacava", 1},
		{"topknobs pulls", "Top-knobs", 1},
		{"Top Knobs pulls", "Top-knobs", 1},
		{"sub zero fridge", "Sub-zero", 1},
		{"native trails trough", "Native-trails", 1},
	}
	for _, c := range cases {
		in := ParseIntent(c.q, brands)
		if len(in.Brands) != 1 || in.Brands[0] != c.slug {
			t.Errorf("%q: brands = %v, want %s", c.q, in.Brands, c.slug)
		}
		if len(in.Concepts) != c.words {
			t.Errorf("%q: concepts = %d (%v), want %d", c.q, len(in.Concepts), in.Explain(), c.words)
		}
	}
	if in := ParseIntent("chrome faucet", brands); len(in.Brands) != 0 {
		t.Errorf("no brand expected, got %v", in.Brands)
	}
}
