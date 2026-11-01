package store

import (
	"strings"
	"testing"
)

func TestClassifyFinish(t *testing.T) {
	cases := []struct {
		name, tones, treatment string
	}{
		{"Oil Rubbed Bronze", "bronze", "antique"},
		{"Distressed Oil Rubbed Bronze", "bronze", "antique"},
		{"Venetian Bronze", "bronze", "antique"},
		{"Satin Bronze", "bronze", "satin"},
		{"Dark Bronze", "bronze", ""},
		{"Brushed Nickel", "nickel", "satin"},
		{"Lifetime Satin Nickel", "nickel", "satin"},
		{"Vibrant Brushed Nickel", "nickel", "satin"},
		{"Polished Chrome", "chrome", "polished"},
		{"Matte Black / Luxe Gold", "black gold", "matte"},
		{"Rose Gold", "copper", ""},
		{"Stainless Steel", "stainless", ""},
		{"French Antique", "", "antique"},
		{"Unlacquered Brass", "brass", "unlacquered"},
		{"Non Lacquered Brass", "brass", "unlacquered"},
		{"Flat Black", "black", "matte"},
		{"Gun Metal", "gray", ""},
		{"Tumbled White Bronze", "white bronze", "antique"},
	}
	for _, c := range cases {
		e := ClassifyFinish(c.name)
		if got := strings.Join(e.Tones, " "); got != c.tones || e.Treatment != c.treatment {
			t.Errorf("%s: tones=%q treatment=%q, want %q %q", c.name, got, e.Treatment, c.tones, c.treatment)
		}
	}
}

func testDict() *FinishDict {
	return NewFinishDict(map[string]int{
		"Oil Rubbed Bronze": 1969, "Venetian Bronze": 225, "Antique Bronze": 48, "Distressed Oil Rubbed Bronze": 55,
		"Satin Bronze": 94, "Dark Bronze": 23, "Brushed Bronze": 9, "Flat Black Bronze": 204, "Tumbled White Bronze": 170,
		"Brushed Nickel": 1094, "Satin Nickel": 1715, "Polished Nickel": 1532, "Lifetime Satin Nickel": 58, "Nickel Silver": 8,
		"Polished Chrome": 6240, "Chrome": 754, "Matte Black": 1306, "Flat Black": 640, "Black": 146, "Black Nickel": 93,
		"Polished Brass": 933, "Satin Brass": 358, "Antique Brass": 242, "Luxe Gold": 375, "Rose Gold": 4, "Polished Copper": 141, "Matte Black / Luxe Gold": 63,
		"White": 2789, "Stainless Steel": 1057, "French Antique": 307, "G": 9, "Other": 1,
	})
}

func has(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func TestResolveFinish(t *testing.T) {
	d := testDict()
	cases := []struct {
		q         string
		wantExact []string
		wantIn    []string
		wantOut   []string
	}{
		{"antique rubbed bronze", []string{"Oil Rubbed Bronze", "Venetian Bronze", "Antique Bronze", "Distressed Oil Rubbed Bronze"},
			[]string{"Dark Bronze"}, []string{"Satin Bronze", "Brushed Bronze", "Flat Black Bronze", "Polished Chrome", "Antique Brass"}},
		{"oil rubbed bronze", []string{"Oil Rubbed Bronze"}, []string{"Antique Bronze", "Dark Bronze"}, []string{"Satin Bronze"}},
		{"bronze", nil, []string{"Oil Rubbed Bronze", "Satin Bronze", "Dark Bronze"}, []string{"Antique Brass", "Chrome", "Flat Black Bronze"}},
		{"dark bronze", []string{"Dark Bronze"}, []string{"Oil Rubbed Bronze"}, []string{"Chrome"}},
		{"black", nil, []string{"Matte Black", "Black Nickel", "Flat Black Bronze"}, []string{"Polished Nickel"}},
		{"gun metal", nil, nil, []string{"Polished Nickel"}},
		{"brushed nickel", []string{"Brushed Nickel", "Satin Nickel", "Lifetime Satin Nickel"}, nil, []string{"Polished Nickel", "Black Nickel", "Nickel Silver"}},
		{"satin nickel", []string{"Satin Nickel", "Brushed Nickel"}, nil, []string{"Polished Nickel"}},
		{"matte black", []string{"Matte Black", "Flat Black"}, []string{"Black", "Black Nickel"}, []string{"Polished Chrome"}},
		{"black and gold", nil, []string{"Matte Black / Luxe Gold", "Luxe Gold", "Matte Black"}, []string{"Polished Chrome"}},
		{"silver", nil, []string{"Polished Chrome", "Brushed Nickel", "Stainless Steel"}, []string{"Polished Brass", "Oil Rubbed Bronze"}},
		{"gold", nil, []string{"Luxe Gold", "Polished Brass", "Antique Brass"}, []string{"Rose Gold", "Polished Chrome"}},
		{"rose gold", []string{"Rose Gold"}, []string{"Polished Copper"}, []string{"Luxe Gold"}},
		{"antique", []string{"French Antique"}, []string{"Oil Rubbed Bronze", "Antique Brass"}, []string{"Dark Bronze", "Polished Chrome"}},
		{"polished chrome", []string{"Polished Chrome"}, []string{"Chrome"}, []string{"Brushed Nickel"}},
		{"stainless steel", []string{"Stainless Steel"}, nil, []string{"Chrome"}},
	}
	for _, c := range cases {
		in := ParseIntentDict(c.q, nil, d)
		fc := in.FinishConcept()
		if fc == nil || len(in.Concepts) != 1 {
			t.Errorf("%q: concepts = %v, want one finish", c.q, in.Explain())
			continue
		}
		for _, n := range c.wantExact {
			if !has(fc.Exact, n) {
				t.Errorf("%q: exact %v lacks %s", c.q, fc.Exact, n)
			}
		}
		for _, n := range c.wantIn {
			if !has(fc.Names, n) {
				t.Errorf("%q: names %v lack %s", c.q, fc.Names, n)
			}
		}
		for _, n := range c.wantOut {
			if has(fc.Names, n) {
				t.Errorf("%q: names %v should not include %s", c.q, fc.Names, n)
			}
		}
		if has(fc.Names, "G") || has(fc.Names, "Other") {
			t.Errorf("%q: junk finish in %v", c.q, fc.Names)
		}
		for _, p := range fc.Patterns {
			if len(p) < 4 {
				t.Errorf("%q: short pattern %q", c.q, p)
			}
		}
	}
}

func TestFinishRunWithTypes(t *testing.T) {
	d := testDict()
	cases := []struct {
		q      string
		finish string // label of the finish concept
		types  int
	}{
		{"oil rubbed bronze kitchen faucet", "oil rubbed bronze", 1},
		{"brushed nickel towel bar", "brushed nickel", 1},
		{"stainless steel sink", "stainless steel", 1},
		{"black knob", "black", 1},
		{"white toilet", "white", 1},
		{"faucet in matte black", "matte black", 1},
		{"soft close hinge", "", 1},
		{"mirror", "", 1},
		{"copper", "copper", 0},
	}
	for _, c := range cases {
		in := ParseIntentDict(c.q, nil, d)
		fc := in.FinishConcept()
		label := ""
		if fc != nil {
			label = fc.Label
		}
		if label != c.finish {
			t.Errorf("%q: finish %q, want %q (%s)", c.q, label, c.finish, in.Explain())
		}
		n := 0
		for _, x := range in.Concepts {
			if x.Kind == "type" {
				n++
			}
		}
		if n != c.types {
			t.Errorf("%q: %d type concepts, want %d (%s)", c.q, n, c.types, in.Explain())
		}
	}
	if in := ParseIntentDict("antique rubbed bronze", nil, d); !strings.Contains(in.Explain(), "Oil Rubbed Bronze") {
		t.Errorf("explain: %s", in.Explain())
	}
}
