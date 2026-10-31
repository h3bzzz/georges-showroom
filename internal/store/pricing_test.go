package store

import "testing"

func TestFinishCodeKey(t *testing.T) {
	codes := []string{"BNX", "PNTK", "BN", "NK", "PC", "RB", "SL", "BL", "GL"}
	cases := []struct {
		sku, key string
		ok       bool
	}{
		{"RP71650NK", "RP71650", true},
		{"RP71650BNX", "RP71650", true}, // longest code wins over BN
		{"81375-BN", "81375", true},
		{"T60085-PNTK", "T60085", true},
		{"RP71650", "", false},   // no code
		{"HK6006", "", false},    // not a finish suffix
		{"T60085ABL", "", false}, // glued after a letter: part of the model
		{"BL", "", false},        // code alone
		{"ABNK", "", false},      // no digit in the model
	}
	for _, c := range cases {
		k, ok := finishCodeKey(c.sku, codes)
		if k != c.key || ok != c.ok {
			t.Errorf("finishCodeKey(%q) = %q,%v want %q,%v", c.sku, k, ok, c.key, c.ok)
		}
	}
}

func TestWantsParts(t *testing.T) {
	brands := map[string]string{"brizo": "brizo"}
	if ParseIntent("brushed nickel", brands).WantsParts() {
		t.Error("a finish query must not ask for parts")
	}
	if ParseIntent("brushed nickel faucet", brands).WantsParts() {
		t.Error("a fixture query must not ask for parts")
	}
	if !ParseIntent("brizo rough valve", brands).WantsParts() {
		t.Error("a valve query asks for parts")
	}
	if !ParseIntent("cartridges", brands).WantsParts() {
		t.Error("plural part type")
	}
}
