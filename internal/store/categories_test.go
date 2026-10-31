package store

import "testing"

func TestCategoryFor(t *testing.T) {
	cases := []struct{ room, title, want string }{
		{"bathroom", "Wall Mount Shower Arms", "Shower Arms"},
		{"bathroom", "Shower Arm and Flange", "Faucet Parts"},
		{"bathroom", "For use with MultiChoice Universal rough", "Rough-In Valves"},
		{"bathroom", "Raincan Shower Heads", "Shower Heads"},
		{"bathroom", "Architec Two Piece Toilet", "Toilets"},
		{"bathroom", "Single Hole Bidet polished chrome", "Bathroom Sink Faucets"},
		{"kitchen", "Pull-Down Prep Sink mixer", "Kitchen Faucets"},
		{"kitchen", "Biscay Fluted Single Bowl Teak Farmer Sink", "Kitchen Sinks"},
		{"bathroom", "Traditional Wall Mount Tub Filler", "Tub Fillers"},
		{"hardware", "Surface Mount Back to Back Pair", ""},
		{"hardware", "Estate Knob", "Knobs"},
		{"bathroom", "Zencha Vanity Cabinet", "Vanities"},
		{"bathroom", "Single Ended Toilet Paper Holder", "Paper Holders"},
		{"bathroom", "Double Bath Hook", "Towel Bars & Rings"},
	}
	for _, c := range cases {
		if got := CategoryFor(c.room, c.title, ""); got != c.want {
			t.Errorf("CategoryFor(%q) = %q want %q", c.title, got, c.want)
		}
	}
	if got := CategoryForSKU("BRZ", "HLK175", "kitchen", "Outdoor Kitchen Faucet", ""); got != "Faucet Parts" {
		t.Errorf("Brizo handle kit: %q", got)
	}
	if got := CategoryForSKU("BRZ", "61325LF", "kitchen", "Articulating Faucet Pull-Down Faucet", ""); got != "Kitchen Faucets" {
		t.Errorf("Brizo faucet: %q", got)
	}
	if p := categoryPattern("Shower Arms"); p != `\mshowers?\s+arms?` {
		t.Errorf("pattern %q", p)
	}
	if p := categoryPattern("Vanities"); p != `\mvanit(y|ies)` {
		t.Errorf("pattern %q", p)
	}
}

func TestTaxonomy(t *testing.T) {
	if got := CategoriesForType("faucets"); len(got) == 0 || got[0] != "Bathroom Sink Faucets" {
		t.Errorf("faucets → %v", got)
	}
	if got := CategoriesForType("shower head"); len(got) != 1 || got[0] != "Shower Heads" {
		t.Errorf("shower head → %v", got)
	}
	if got := categoriesForFilter("Toilets"); len(got) != 3 {
		t.Errorf("Toilets group → %v", got)
	}
	if got := CategoryFor("bathroom", "Built in faucet deck", ""); got != "Bathtub Parts" {
		t.Errorf("faucet deck → %q", got)
	}
	if got := CategoryFor("kitchen", "Faucet & Everhot Tank", ""); got != "Water Filtration" {
		t.Errorf("everhot → %q", got)
	}
	if got := CategoryFor("hardware", "Single Ended Toilet", ""); got != "Bath Accessories" {
		t.Errorf("hardware toilet → %q", got)
	}
	if got := CategoryFor("kitchen", "System includes faucet, tank, cord and tubing POLISHED NICKEL", ""); got != "Water Filtration" {
		t.Errorf("tank system → %q", got)
	}
	if sc := scoreTitle("WH Polished brass Tub wall-mounted faucets for use with risers"); sc >= 0 {
		t.Errorf("footnote line scored %d", sc)
	}
}
