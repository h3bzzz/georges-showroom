package titles

import "testing"

func TestSynthesize(t *testing.T) {
	cases := []struct {
		in   Input
		want string
	}{
		{Input{Title: "SH-PB1-15-033", SKU: "SH-PB1-15-033", SpecText: "Shower Systems SH-PB1-15-033 Product Type Pressure balancing shower fitting with Auto-Drain® Features & Specifications Job Name"},
			"Pressure balancing shower fitting with Auto-Drain® SH-PB1-15-033"},
		{Input{Title: "LEF-B51", SKU: "LEF-B51-00", SpecText: "Laboratory Fittings LEF-B51-00 Product Type Touchless electronic faucet Features & Specifications"},
			"Touchless electronic faucet LEF-B51-00"},
		{Input{Title: "SOLID BRASS", SKU: "MT3880-032", Category: "Knobs", Shared: 122}, "Solid Brass Knob MT3880-032"},
		{Input{Title: "NORDIC COLLECTION", SKU: "MA5554-032", Shared: 30}, "Nordic Collection · MA5554-032"},
		{Input{Title: "RP101286RB", SKU: "RP101286RB", Category: "Faucet Parts"}, "Faucet Part RP101286RB"},
		{Input{Title: "A265", SKU: "A265", Category: "Bathroom Sinks", Collection: "Fonte"}, "Fonte Bathroom Sink A265"},
		{Input{Title: "Bridge Faucet with Side Sprayer", SKU: "62536LF-PC", Category: "Kitchen Faucets"}, ""},
		{Input{Title: "Air Gap", SKU: "69060-BL", Shared: 1}, ""},
		{Input{Title: "Smooth Lever Carbon Fiber Lever Knurled Lever", SKU: "3002X", Category: "Levers", Shared: 20}, ""},
		{Input{Title: "Pull Wine", SKU: "MB-15", Category: "Cabinet Pulls", Shared: 6}, "Pull Wine · MB-15"},
		{Input{Title: "FS313", SKU: "FS313"}, ""},
		{Input{Title: "RP78583RB", Category: "Faucet Parts"}, "Faucet Part RP78583RB"},
		{Input{Title: "2940-5223", SKU: "2940-5223", Category: "Kitchen Faucets", Facts: map[string]string{"Product Type": "Pull-down prep faucet"}}, "Pull-down prep faucet 2940-5223"},
	}
	for _, c := range cases {
		if got := Synthesize(c.in); got != c.want {
			t.Errorf("Synthesize(%q) = %q, want %q", c.in.Title, got, c.want)
		}
	}
	if Singular("Kitchen Faucets") != "Kitchen Faucet" || Singular("Vanities") != "Vanity" || Singular("Deadbolts & Locks") != "Deadbolt" || Singular("Glass") != "Glass" {
		t.Error("Singular")
	}
	if !Codey("SH-PB1-15-033", "") || Codey("Solid Brass", "") || !Codey("ABC", "") {
		t.Error("Codey")
	}
}
