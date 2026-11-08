package vendors

import (
	"strings"
	"testing"
)

func TestCalCoverSwap(t *testing.T) {
	codes := calCodes(nil)
	cases := map[string]string{
		"https://images.calfaucets.com/covers/_productThumb/6502-SN.jpg": "6502-PC.jpg",
		"https://x/_productThumb/TO-THRR1-30F_vert-SN.jpg":               "TO-THRR1-30F_vert-PC.jpg",
		"https://x/_productThumb/K51-100SQ-FB-SN.jpg":                    "K51-100SQ-FB-PC.jpg",
		"https://x/_productThumb/SH-04T-FR-SN-1.jpg":                     "SH-04T-FR-PC-1.jpg",
		"https://x/_productThumb/single-hole-vessel-bathroom-faucet.jpg": "",
		"https://x/_productThumb/SB-110-XX.jpg":                          "",
	}
	for cover, want := range cases {
		if got := calCoverSwap(cover, "PC", codes); got != want {
			t.Errorf("%s: got %q want %q", cover, got, want)
		}
	}
	cat := map[string]calProduct{"6502": {Model: "6502", Image: "https://images.calfaucets.com/covers/_productThumb/6502-SN.jpg"}}
	_, urls := calCandidates("6502ZBF-SC", codes, cat)
	if len(urls) != 3 || urls[0] != "https://images.calfaucets.com/catalog/_productLarge/6502-SC.jpg" {
		t.Fatalf("candidates %v", urls)
	}
}

func TestCalURLs(t *testing.T) {
	codes := calCodes([]string{"SC", "SN", "RS"})
	cases := map[string][]string{
		"6502ZBF-SC":    {"https://images.calfaucets.com/catalog/_productLarge/6502ZBF-SC.jpg", "https://images.calfaucets.com/catalog/_productLarge/6502-SC.jpg"},
		"TO-4508X-SC":   {"https://images.calfaucets.com/catalog/_productLarge/TO-4508X-SC.jpg"},
		"3001-1T.18-SN": {"https://images.calfaucets.com/catalog/_productLarge/3001-1T.18-SN.jpg", "https://images.calfaucets.com/catalog/_productLarge/3001-1T.FR-SN.jpg"},
		"4101-2-YP":     nil, // YP is an accent handle, not a finish
		"34-TP":         nil,
	}
	for sku, want := range cases {
		_, got := calURLs(sku, codes)
		if len(got) != len(want) {
			t.Fatalf("%s: got %v want %v", sku, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s[%d]: got %s want %s", sku, i, got[i], want[i])
			}
		}
	}
}

func TestBaldwinCandidates(t *testing.T) {
	var b baldwinSource
	if c, u := b.candidates("8232.050"); c != "050" || len(u) != 1 || !strings.Contains(u[0], "/Baldwin/8232-050-c1?") {
		t.Fatalf("%s %v", c, u)
	}
	if c, _ := b.candidates("8040.003.0510"); c != "003" {
		t.Fatal("numeric option should be accepted")
	}
	if c, u := b.candidates("MP001.031.ACT1"); c != "031" || !strings.Contains(u[0], "/Baldwin/mp001-031-c1?") {
		t.Fatal("option code is dropped from the image id")
	}
	if c, _ := b.candidates("RID.R.CFR.260"); c != "" {
		t.Fatal("a SKU without a 3-digit finish in second place is skipped")
	}
}
