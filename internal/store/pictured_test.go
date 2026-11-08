package store

import "testing"

func TestPicturedFinish(t *testing.T) {
	codes := map[string]string{"SN": "Satin Nickel", "PC": "Polished Chrome", "RB": "Venetian Bronze", "B1": ""}
	delete(codes, "B1")
	cases := map[string]string{
		"https://images.calfaucets.com/catalog/_productLarge/3001-1T-SN.jpg": "Satin Nickel",
		"https://media.brizo.com/elvis/OnWhite/lg/T67430-RB-B1.png":          "Venetian Bronze",
		"https://images.calfaucets.com/catalog/_productLarge/3109-2.jpg":     "",
		"https://x/SN-foo.jpg": "", // a leading token is the model, never the finish
		"":                     "",
	}
	for u, want := range cases {
		if got := picturedFinish(codes, u); got != want {
			t.Errorf("%s: got %q want %q", u, got, want)
		}
	}
	if !sameFinish("Polished  Chrome", "polished chrome") || sameFinish("Satin Nickel", "Polished Nickel") {
		t.Error("sameFinish")
	}
}
