package ocr

import "testing"

func TestLooksLikeSKU(t *testing.T) {
	yes := []string{"37231", "K30-123-TX", "NSL3619-CX", "54112-726", "E501FB-1", "1206-41", "TRAN71DS-CLR", "29770"}
	no := []string{"7-15/16", "18-13/16", "1,329", "2026", "031", "$1,329", "360", "1.75", "9/16", "Chrome", "ADA", "1-5/16", "57MM", "1.75GPM"}
	for _, s := range yes {
		if !LooksLikeSKU(s) {
			t.Errorf("%q should look like a SKU", s)
		}
	}
	for _, s := range no {
		if LooksLikeSKU(s) {
			t.Errorf("%q should NOT look like a SKU", s)
		}
	}
}
