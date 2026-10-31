package pricing

import (
	"math"
	"testing"
)

func TestParseCents(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"$1,787.00", 178700, true},
		{"2,681.00", 268100, true},
		{"327.00", 32700, true},
		{"$450", 45000, true},
		{"$ 450", 45000, true},
		{"$12,345", 1234500, true},
		{"1,234.50*", 123450, true},
		{"186,193", 0, false}, // index page: pages 186 and 193
		{"133,48", 0, false},  // index page: pages 133 and 48
		{"1,234", 0, false},   // bare thousands comma: ambiguous, rejected
		{"245", 0, false},     // page number
		{"20099", 0, false},   // a Gessi SKU
		{"6.29", 0, false},    // a flow rate, below MinCents
		{"$2.50", 0, false},
		{"$186,193.00", 0, false}, // above MaxCents
		{"1.234,00", 0, false},
		{"", 0, false},
		{"$", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseCents(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseCents(%q) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFence(t *testing.T) {
	// a realistic faucet book: log-spread from a $30 aerator to a $6,000 tub filler, with a few
	// wild OCR reads mixed in (index references and SKUs read as money)
	var prices []int64
	for i := 0; i < 200; i++ {
		prices = append(prices, int64(3000*math.Pow(200, float64(i)/199)))
	}
	bad := []int64{18619300, 13003700, 9924000, 7479200}
	f := NewFence(append(prices, bad...))
	if f.TooSmall {
		t.Fatal("fence should have a sample")
	}
	for _, p := range prices {
		if !f.Plausible(p) {
			t.Errorf("genuine price %d outside fence [%d,%d]", p, f.Lo, f.Hi)
		}
	}
	for _, p := range bad {
		if f.Plausible(p) {
			t.Errorf("wild price %d inside fence [%d,%d]", p, f.Lo, f.Hi)
		}
	}
	small := NewFence([]int64{1000, 2000})
	if !small.TooSmall || small.Lo != MinCents || small.Hi != MaxCents {
		t.Errorf("small sample should fall back to hard bounds: %+v", small)
	}
	flatVals := make([]int64, 30)
	for i := range flatVals {
		flatVals[i] = 50000
	}
	flat := NewFence(flatVals)
	if !flat.Plausible(2600) || !flat.Plausible(990000) || flat.Plausible(1100000) || flat.Plausible(2400) {
		t.Errorf("flat book should fence ×20 around the median: %+v", flat)
	}
}
