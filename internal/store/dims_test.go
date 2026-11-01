package store

import (
	"reflect"
	"testing"
)

func TestParseInches(t *testing.T) {
	cases := map[string][]float64{
		`8"`:                  {8},
		`8-3/8"`:              {8.38},
		`5/8"`:                {0.63},
		`9.5"`:                {9.5},
		`96 mm`:               {3.78},
		`60 cm`:               {23.62},
		`32-1/4" x 18-7/8"`:   {32.25, 18.88},
		`10''`:                {10},
		`1-1/2" NPT`:          {1.5},
		`5 1/4"`:              {5.25},
		`66`:                  {66},
		`Super Single`:        nil,
		`1.6 / 0.8 gpf`:       {1.6, 0.8},
		`60" x 32" x 22-1/2"`: {60, 32, 22.5},
	}
	for in, want := range cases {
		if got := ParseInches(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInches(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseDimQuery(t *testing.T) {
	cases := []struct {
		q        string
		inches   float64
		keys     []string
		consumed bool
	}{
		{"66 inch tub", 66, nil, true},
		{`66" freestanding tub`, 66, nil, true},
		{"8 inch centers faucet", 8, []string{"Centers"}, true},
		{"faucet 8\" centers", 8, []string{"Centers"}, true},
		{"30 inch wide vanity", 30, dimHints["wide"], true},
		{"5-1/2 inch pull", 5.5, nil, true},
		{"5 1/2 in centers", 5.5, []string{"Centers"}, true},
		{"K30-123", 0, nil, false},
		{"2 handle faucet", 0, nil, false},
		{"120 mm pull", 4.72, nil, true},
		{"10in reach", 10, []string{"Spout Reach"}, true},
	}
	for _, c := range cases {
		in := ParseIntent(c.q, nil)
		var got *Concept
		for i := range in.Concepts {
			if in.Concepts[i].Kind == "dim" {
				got = &in.Concepts[i]
			}
		}
		if (got != nil) != c.consumed {
			t.Errorf("%q: dim concept present=%v want %v (%+v)", c.q, got != nil, c.consumed, in.Concepts)
			continue
		}
		if got == nil {
			continue
		}
		if got.Inches != c.inches {
			t.Errorf("%q: inches %v want %v", c.q, got.Inches, c.inches)
		}
		if c.keys != nil && !reflect.DeepEqual(got.DimKeys, c.keys) {
			t.Errorf("%q: keys %v want %v", c.q, got.DimKeys, c.keys)
		}
	}
}
