package toc

import "testing"

func TestFold(t *testing.T) {
	heads := map[int]string{
		3: "TABLE of CONTENTS", 4: "Kitchen Collections", 5: "Bath Collections", 6: "surprising inspiring",
		16: "Artesso Kitchen", 17: "Kitchen", 20: "installation", 22: "Frank Lloyd Wright", 24: "deposits",
		25: "Description", 26: "are ADA", 28: "Kintsu Kitchen", 29: "wand spray and", 30: "Model", 31: "Kintsu",
		36: "Litze Kitchen", 37: "Seal", 41: "with", 42: "spout", 52: "Odin Kitchen", 58: "No No",
		62: "Jason Wu For Brizo", 70: "Rook Kitchen", 71: "Rook", 76: "Tulham Kitchen", 90: "Lacava",
		100: "charlotte", 110: "Finishes", 84: "Delivers near boiling water on dem", 85: "Effortless harmony",
		104: "Hole", 105: "Ermostat", 106: "Chrome Cr", 107: "High-", 108: "HOUSEPURE soFTENER", 112: "Ventaglio", 113: "Ventaglio",
		116: "Baliza Bath", 118: "Baliza", 119: "SURFACE OTHERi",
	}
	got := Fold(heads, 120, Options{Brand: "Lacava", Collections: []string{"Artesso", "Kintsu", "Charlotte", "Rook", "Baliza"}, Dict: map[string]bool{"finishes": true, "kitchen": true}})
	want := []Entry{
		{4, "Kitchen Collections", 1}, {5, "Bath Collections", 1}, {16, "Artesso Kitchen", 2}, {22, "Frank Lloyd Wright", 2},
		{28, "Kintsu Kitchen", 2}, {36, "Litze Kitchen", 2}, {52, "Odin Kitchen", 2}, {62, "Jason Wu For Brizo", 2},
		{70, "Rook Kitchen", 2}, {76, "Tulham Kitchen", 2}, {100, "Charlotte", 2}, {110, "Finishes", 1},
		{112, "Ventaglio", 2}, {116, "Baliza Bath", 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %+v\nwant %+v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	flat := Fold(map[int]string{2: "Aquagrande Sinks", 5: "Aquagrande", 9: "Libera Sinks"}, 12, Options{})
	if len(flat) != 2 || flat[0].Level != 1 || flat[1].Title != "Libera Sinks" {
		t.Errorf("flat = %+v", flat)
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"S-AS 38. ¢ % : s": "S-AS 38", "TDLFS5 © oo": "TDLFS5", "New Products . i 8§ 3 i": "New Products",
		"Davoli · K10 Series": "Davoli · K10 Series", "FFAS 38 «": "FFAS 38", "Site Sizable® PVC (S)": "Site Sizable® PVC (S)",
	} {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
