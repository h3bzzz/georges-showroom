package store

import "testing"

func TestSKUBase(t *testing.T) {
	cases := map[string]string{"5399.XXX.D": "5399", "K30-123-XX": "K30-123", "37231": "37231", "SH-177-8.XX-MBLK": "SH-177-8", "8244.112.0510": "8244.112.0510", "5452V": "5452V"}
	for in, want := range cases {
		if got := SKUBase(in); got != want {
			t.Errorf("SKUBase(%q) = %q, want %q", in, got, want)
		}
	}
}
