package store

import (
	"strings"
	"testing"
)

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{
		`length 2" Double Robe Hook A6684 $ 95.00`:                       "Double Robe Hook",
		`A9260-PB ROBE HOOK ROBE HOOK`:                                   "Robe Hook",
		`wide H_ 8oz Robe Hook §7495 A6830`:                              "Robe Hook",
		`62858LF-PC 1175.00 + Two handle wall mount @`:                   "Two handle wall mount",
		`A265UCX 5-Hole Roman tub set - Handles in Carrara white Marble`: "5-Hole Roman tub set Handles in Carrara white Marble",
		`Toz Double Robe Hook A9086`:                                     "Double Robe Hook",
	}
	for in, want := range cases {
		sku := ""
		for _, w := range []string{"A6684", "A9260-PB", "A6830", "62858LF-PC", "A265UCX", "A9086"} {
			if strings.Contains(in, w) {
				sku = w
			}
		}
		if got := cleanTitle(in, sku, 0); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
