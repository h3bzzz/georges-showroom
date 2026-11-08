package search

import "testing"

func TestParse(t *testing.T) {
	b := NewBrandIndex([]string{"brizo", "newport-brass", "california-faucets"}, []string{"Brizo", "Newport Brass", "California Faucets"})
	in := Parse("newport brass brushed nickel kitchen faucet", b)
	if in.Brand != "newport-brass" {
		t.Fatalf("brand %q", in.Brand)
	}
	if in.Finish == nil || in.Finish.Treatment != "satin" || len(in.Finish.Tones) != 1 || in.Finish.Tones[0] != "nickel" {
		t.Fatalf("finish %+v", in.Finish)
	}
	if len(in.Categories) != 1 || in.Categories[0] != "Kitchen Faucets" {
		t.Fatalf("cats %v", in.Categories)
	}
	if in.Text != "" {
		t.Fatalf("text %q", in.Text)
	}
	in = Parse("T60P-6005 matte black", b)
	if in.SKU != "T60P-6005" || in.Finish == nil || in.Finish.Tones[0] != "black" {
		t.Fatalf("%+v", in)
	}
	for _, q := range []string{"K-50", "k-50", "65306LF", "2940-5223", "K50-100-45-PC", "R-3550"} {
		if in := Parse(q, b); in.SKU == "" || in.Text != "" {
			t.Errorf("%q should parse as a model number: %+v", q, in)
		}
	}
	if in := Parse("bath", b); in.SKU != "" {
		t.Errorf("bath is not a model number: %+v", in)
	}
	in = Parse("odin pot filler", b)
	if in.Text != "odin" || in.Categories[0] != "Pot Filler Faucets" {
		t.Fatalf("%+v", in)
	}
}
