package ocr

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLinesAndCanonOnPriceList(t *testing.T) {
	b, err := os.ReadFile("../../data/raw/catalogs/ABW/ABW_2024Pricelist/0057.words.json")
	if err != nil {
		t.Skip("fixture not present")
	}
	var words []Word
	if err := json.Unmarshal(b, &words); err != nil {
		t.Fatal(err)
	}
	c := NewCanon()
	c.Add(57, words)
	// page 46 also prints A008WU; emulate by adding the header twice
	c.Add(46, words)
	gotTitle, prices := false, 0
	for _, ln := range Lines(words) {
		for _, k := range ln.SKUs {
			sku, ok := c.Resolve(57, k)
			if !ok || sku != "A008WU" {
				continue
			}
			if ln.PriceCents > 0 {
				prices++
			}
			if containsAll(ln.Text, "Single-control", "bidet", "mixer") {
				gotTitle = true
			}
		}
	}
	if !gotTitle {
		t.Errorf("header row did not join the description")
	}
	if prices < 3 {
		t.Errorf("expected ≥3 priced finish rows for A008WU, got %d", prices)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !contains(s, x) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
