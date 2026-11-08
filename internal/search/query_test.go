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
	in = Parse("odin pot filler", b)
	if in.Text != "odin" || in.Categories[0] != "Pot Filler Faucets" {
		t.Fatalf("%+v", in)
	}
}
