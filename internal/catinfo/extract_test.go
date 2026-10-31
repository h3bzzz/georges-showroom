package catinfo

import (
	"strings"
	"testing"
)

func factMap(r Result) map[string]string {
	m := map[string]string{}
	for _, f := range r.Facts {
		m[f.Key] = f.Value
	}
	return m
}

// one rule, one real OCR line (or a few lines of one block), the facts it must yield
func TestRules(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want map[string]string
		none []string // keys that must be absent
	}{
		{"brizo roman tub block", Input{Brand: "BRZ", BrandName: "Brizo", Room: "bathroom", Category: "Tub Fillers", SKUs: []string{"T67385-PCLHP"},
			Own: []string{`T67385-PCLHP 1093.00 « 3-hole installation`},
			Block: []string{`T67385-PCLHP 1093.00 « 3-hole installation`, `T67385-PNLHP 1312.00 « Overall height = 91/8", spout outlet to`, `deck = 6", spout reach = 9"`,
				`+ Accommodates up to21/2" deck`, `thickness`, `+ 20 gpm @ 60psi, 76 L/min @ 414kPa`, `+ MUST ORDER ROUGH SEPARATELY`, `+ MUST ORDER HANDLES`, `SEPARATELY`}},
			map[string]string{"Faucet Holes": "3", "Overall Height": `9-1/8"`, "Spout Height": `6"`, "Spout Reach": `9"`, "Flow Rate": "20 gpm @ 60 psi", "Max Deck Thickness": `2-1/2"`,
				"Requires": "Rough-in valve (sold separately); Handles (sold separately)"}, nil},
		{"brizo hand shower flow with lost decimal", Input{Brand: "BRZ", Room: "bathroom", Category: "Tub Fillers",
			Block: []string{`« Single-function hand shower: 1.75 gpm`, `@80 psi, 6.6 L/min @ 550 kPa`, `+ 60"-82" stretchable, finished metal`, `hose`, `+ WaterSense® labeled hand shower`}},
			map[string]string{"Flow Rate": "1.75 gpm @ 80 psi", "Hose Length": `60" - 82"`, "Certifications": "WaterSense"}, nil},
		{"brizo tub filler 18.5 gpm checked against L/min", Input{Brand: "BRZ", Room: "bathroom", Block: []string{`+ 18.5gpm @ 60 psi, 70 L/min @ 414 kPa`}},
			map[string]string{"Flow Rate": "18.5 gpm @ 60 psi"}, nil},
		{"brizo ada handles", Input{Brand: "BRZ", Room: "bathroom", Category: "Faucet Parts", Block: []string{`HL685-PC 217.00 + Allhandles are ADA compliant`, `HL685-PN 260.08 + Model number = one pair of handles`}},
			map[string]string{"ADA Compliant": "Yes"}, nil},
		{"brizo diverter spout", Input{Brand: "BRZ", Room: "bathroom", Category: "Tub Fillers", Block: []string{`RP70908PN 533.00 - 83/8"reach`, `« Solid brass construction`, `+ Thread onto1/2" NPT using included adapter (RP49349)`}},
			map[string]string{"Spout Reach": `8-3/8"`, "Material": "Solid Brass", "Inlet": `1/2" NPT`}, nil},
		{"duravit vessel sink", Input{Brand: "DRV", BrandName: "Duravit", Room: "bathroom", Category: "Bathroom Sinks", SKUs: []string{"2354600041"},
			Own:   []string{`2354600041 1faucet hole White Special Order 1240.00`, `235460 235/8"x181/2" -`},
			Block: []string{`DuraSquare Vessel Sink Design by Duravit`, `- Drain included`, `+ Includes mounting hardware`, `+ Without overflow`, `- Rectangular sink made of high quality Duravit ceramic`, `- ADA Compliant`, `- cUPC listed`}},
			map[string]string{"Faucet Holes": "1", "Dimensions": `23-5/8" x 18-1/2"`, "Availability": "Special order", "Drain Included": "Yes", "Overflow": "No", "Material": "Ceramic",
				"ADA Compliant": "Yes", "Certifications": "cUPC", "Mounting": "Vessel"}, []string{"Designer"}},
		{"duravit no holes stock", Input{Brand: "DRV", BrandName: "Duravit", Room: "bathroom", Category: "Bathroom Sinks", Own: []string{`235560 2355600000 235/8"x135/8" No faucet holes White Stock 1120.00`}},
			map[string]string{"Faucet Holes": "None", "Availability": "Stock item", "Dimensions": `23-5/8" x 13-5/8"`}, nil},
		{"duravit bidet designer", Input{Brand: "DRV", BrandName: "Duravit", Room: "bathroom", Category: "Bidets", Own: []string{`2289100000 ‘White Stock 650.00`},
			Block: []string{`ME by Starck Single Hole Horizontal Spray Floorstanding Bidet Design by Philippe Starck`, `- Floorstanding installation`, `- Horizontal drain outlet`, `- CUPC listed`, `- Bidet faucet sold separately`}},
			map[string]string{"Designer": "Philippe Starck", "Mounting": "Freestanding", "Faucet Holes": "1", "Certifications": "cUPC"}, nil},
		{"california faucets tub spout", Input{Brand: "CAL", Room: "bathroom", Category: "Tub Fillers", SKUs: []string{"TS-30-30"},
			Own: []string{`TS-30-30 Wall Tub Spout polished chrome 335`},
			Block: []string{`TS-30-30 Wall Tub Spout polished chrome 335`, `« 6-5/16" wall to center standard 419`, `« max flow rate: |6gpm at 60 psi special 469`, `« for series: 30, 30F 31,31F, 80 & 81`, `premium 553`,
				`+ for use with StyleTherm, StyleTherm Vista, PB2-R or 2-VR valves`}},
			map[string]string{"Spout Reach": `6-5/16"`, "Flow Rate": "16 gpm @ 60 psi", "Works With": "30, 30F 31,31F, 80 & 81; StyleTherm, StyleTherm Vista, PB2-R or 2-VR valves"}, nil},
		{"california faucets misread inlet rejected", Input{Brand: "CAL", Room: "bathroom", Category: "Tub Fillers", Block: []string{`« 112" FIP inlet special 508`, `REQUIRES ROUGH VALVE: 2-VR 469`}},
			map[string]string{"Works With": "2-VR"}, []string{"Inlet"}},
		{"california faucets trim handles and pipe-one flow", Input{Brand: "CAL", Room: "bathroom", Category: "Shower Faucet Trims", Own: []string{`2 Handle Tub or Shower Trim Only polished chrome 578`},
			Block: []string{`* max flow rate: |.8 gpm`, `+ spout max flow rate: 10 gom at 60 psi`, `ceramic disc cartridge`}},
			map[string]string{"Handles": "2", "Flow Rate": "1.8 gpm", "Cartridge": "Ceramic Disc"}, nil},
		{"robern cabinet dims", Input{Brand: "ROB", BrandName: "Robern", Room: "bathroom", Category: "Medicine Cabinets", Heading: `R3" SERIES MEDICINE CABINETS`,
			Own: []string{`RC1620D4FP1 Polished Edge, Reversible Hinge $449 $599`}, Block: []string{`16"W x 20"H x 4"D`, `RC1620D4FP1 Polished Edge, Reversible Hinge $449 $599`}},
			map[string]string{"Width": `16"`, "Height": `20"`, "Depth": `4"`}, []string{"Dimensions"}},
		{"chicago lab faucet", Input{Brand: "CHI", BrandName: "Chicago Faucets", Room: "kitchen", Category: "Kitchen Faucets",
			Block: []string{`Deck Mounted, 8" Fixed Centers,`, `with 5-1/4" Gooseneck Spout`, `Solid brass construction`, `Polished chrome plated finish`, `* 1/2"NPSM supply inlets and coupling nut for 3/8" or 1/2" flexible riser`, `2.2 GPM Aerated Outlet`}},
			map[string]string{"Mounting": "Deck Mount", "Centers": `8"`, "Spout Reach": `5-1/4"`, "Material": "Solid Brass", "Inlet": `1/2" NPSM`, "Flow Rate": "2.2 gpm", "Spout Type": "Gooseneck"}, nil},
		{"chicago catalog series", Input{Brand: "CHI", BrandName: "Chicago Faucets", Room: "kitchen", Category: "Kitchen Faucets",
			Block: []string{`Deck-mounted manual sink faucet`, `Series: 349, 350`, `« Single hole`, `« Vandal-resistant lever or wristblade handles standard, other options available`, `« Ceramic or Quaturn™ compression cartridges`, `« Polished chrome finish`, `« Flow rates from 1.5 GPM to full flow`}},
			map[string]string{"Series": "349, 350", "Faucet Holes": "1", "Flow Rate": "1.5 gpm to full flow", "Handle Style": "Lever or Wristblade"}, nil},
		{"barclay pedestal", Input{Brand: "BAR", Room: "bathroom", Category: "Bathroom Sinks", Own: []string{`WH | 3-3064WH 4" centerset`},
			Block: []string{`« Vitreous china + Overflow`, `+ Also available as a wall-hung`, `Basin interior; 11%" x 8%" + Basin depth: 54"`, `Mount bolts height: 314" « Mount holes cc: 64"`, `W 145"`, `H 34%"`}},
			map[string]string{"Centers": `4"`, "Faucet Holes": "3", "Material": "Vitreous China", "Overflow": "Yes"}, []string{"Mounting", "Basin Interior", "Basin Depth", "Width", "Height"}},
		{"blanco silgranit row", Input{Brand: "BLA", BrandName: "Blanco", Room: "kitchen", Category: "Kitchen Sinks", Own: []string{`443224 LIVEN 25 Single, SILGRANIT, Volcano Gray $742 63`}},
			map[string]string{"Nominal Size": `25"`, "Bowls": "Single", "Material": "SILGRANIT granite composite"}, nil},
		{"blanco stainless block", Input{Brand: "BLA", BrandName: "Blanco", Room: "kitchen", Category: "Kitchen Sinks",
			Block: []string{`— German engineered with 304 series, 20-gauge stainless steel`, `Bowl depth 9.5in 9.5in 8.66 in 8.66 in`, `Minimum cabinet size 36" 33" 30" 27"`, `Product weight: 33 Ibs`, `— Made in Canada and Germany`, `E Undermount`}},
			map[string]string{"Material": "Stainless Steel", "Gauge": "20-gauge", "Bowl Depth": `9.5"`, "Minimum Cabinet": `36"`, "Made In": "Canada and Germany", "Mounting": "Undermount"}, nil},
		{"aboutwater mixer", Input{Brand: "ABW", Room: "bathroom", Category: "Bathroom Sink Faucets", Own: []string{`A104WU Two-hole washbasin mixer with single-control`},
			Block: []string{`«61/8" spout projection`, `« Without drain`, `« Progressive cartridge`, `- Water flow restricted to 1.2 gpm`, `Matte Gun Metal PVD 27 P5 A104WU $ 2,803.00 C`}},
			map[string]string{"Faucet Holes": "2", "Handles": "1", "Spout Reach": `6-1/8"`, "Drain Included": "No", "Cartridge": "Progressive", "Flow Rate": "1.2 gpm"}, nil},
		{"baldwin latch backset", Input{Brand: "BAL", Room: "hardware", Category: "Door Hardware", Own: []string{`5540.xxx.P Privacy Adjustable Latch 2.375" and 2.75" $108`}},
			map[string]string{"Function": "Privacy", "Backset": `2.375" and 2.75"`}, []string{"Faucet Holes"}},
		{"baldwin handleset", Input{Brand: "BAL", Room: "hardware", Category: "Handle Sets", Own: []string{`85390xxxRENT Single cylinder handleset with lever—RH Entry 6.8 $805`}},
			map[string]string{"Function": "Single Cylinder", "Handing": "Right Hand"}, nil},
		{"classic brass knob set", Input{Brand: "CBR", Room: "hardware", Category: "Knobs", Own: []string{`KNOB SET - PASSAGE`, `(K)14460 2-3/8" (R) 14080 2"`}},
			map[string]string{"Function": "Passage", "Knob Diameter": `2-3/8"`, "Rose Diameter": `2"`}, nil},
		{"hydro systems tub", Input{Brand: "HYS", Room: "bathroom", Category: "Bathtubs", Heading: "ELIZABETH",
			Block: []string{`72"L x 40"W x 27"D - center drain`, `W/O Included`, `Foam fill sound insulator included`, `Includes lumbar support`, `ACRYLIC BATHTUB COLORS`}},
			map[string]string{"Length": `72"`, "Width": `40"`, "Depth": `27"`, "Drain Location": "Center", "Material": "Acrylic"}, nil},
		{"ashley norton projection", Input{Brand: "ASH", Room: "hardware", Category: "Cabinet Pulls", Block: []string{`All items have a projection of 2"`, `Allitems have a handle size of 1 %"`}},
			map[string]string{"Projection": `2"`}, nil},
		{"gessi wall mounted", Input{Brand: "GES", Room: "bathroom", Category: "Shower Heads", Block: []string{`WALL-MOUNTED ADJUSTABLE AND`, `ANTI-LIMESTONE SHOWERHEAD`}},
			map[string]string{"Mounting": "Wall Mount"}, nil},
		{"toilet gpf and rough-in", Input{Brand: "DRV", Room: "bathroom", Category: "Toilets", Block: []string{`- 1.28 gpf single flush`, `- 12" rough-in`, `- Elongated bowl`}},
			map[string]string{"Flush Volume": "1.28 gpf", "Rough-In": `12"`}, nil},
		{"dual flush", Input{Brand: "DRV", Room: "bathroom", Category: "Toilets", Block: []string{`0.8/1.28 gpf dual flush`}},
			map[string]string{"Flush Volume": "0.8 / 1.28 gpf (dual flush)"}, nil},
		{"designer initials kept, truncated designer dropped", Input{Brand: "DRV", BrandName: "Duravit", Room: "bathroom", Category: "Bathroom Sinks",
			Block: []string{`Happy D.2 Plus Vessel Sink Design by Studio F. A. Porsche`, `- cUPC listed`}},
			map[string]string{"Designer": "Studio F. A. Porsche"}, nil},
		{"truncated designer dropped", Input{Brand: "DRV", BrandName: "Duravit", Room: "bathroom", Category: "Vanities", Block: []string{`ensuring a beautiful and durable fixture Design by Pat Finish BA9056 | 311/2" x 215/8"`}},
			map[string]string{}, []string{"Designer"}},
		{"lost decimal flow checked against L/min at two scales", Input{Brand: "BRZ", Room: "bathroom", Category: "Hand Showers", Block: []string{`+ 175 gpm @ 80 psi, 6.6 L/min @ 550 kPa`}},
			map[string]string{"Flow Rate": "1.75 gpm @ 80 psi"}, nil},
		{"colon for dash in a fraction", Input{Brand: "CAL", Room: "bathroom", Category: "Bathroom Sink Faucets", Block: []string{`+ 6:3/4" overal height and Rivulet handles`}},
			map[string]string{"Overall Height": `6-3/4"`}, nil},
		{"overall height after the number", Input{Brand: "CAL", Room: "bathroom", Category: "Bathroom Sink Faucets", Block: []string{`* 10-1/16" overall height * 5-5/16" spout center to center`}},
			map[string]string{"Overall Height": `10-1/16"`, "Spout Reach": `5-5/16"`}, nil},
		{"works with stops at the next product row", Input{Brand: "CAL", Room: "bathroom", Category: "Tub Fillers",
			Block: []string{`+ for use with StyleTherm, StyleTherm Vista, PB2-R or 2-VR valves D-E4-37 Deluxe Wall Tub Spout`, `« fits most popular cold/filtered water units with`}},
			map[string]string{"Works With": "StyleTherm, StyleTherm Vista, PB2-R or 2-VR valves; most popular cold/filtered water units"}, nil},
		{"model number tail is not a dimension", Input{Brand: "CAL", Room: "bathroom", Category: "Faucet Parts", Block: []string{`+ use with flange 9100 (above), 9130-37, 9130-60, 9130-65, 9130-77, 9130-85,`, `for more details, see pages 572-574 THT52D2-R 873`}},
			map[string]string{}, []string{"Width", "Height", "Depth", "Dimensions"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := factMap(Extract(c.in))
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("%s: got %q want %q (all: %v)", k, got[k], v, got)
				}
			}
			for _, k := range c.none {
				if v, ok := got[k]; ok {
					t.Errorf("%s: unexpected %q", k, v)
				}
			}
		})
	}
}

func TestCollection(t *testing.T) {
	cases := []struct{ in, brand, want string }{
		{`Descanso · K30 Series`, "California Faucets", "Descanso"},
		{`R3" SERIES MEDICINE CABINETS`, "Robern", "R3 Series"},
		{`DuraSquare Vessel Sink Design by Duravit`, "Duravit", "DuraSquare"},
		{`ROMAN TUB FAUCETS`, "Brizo", ""},
		{`Allaria’,`, "Brizo", "Allaria"},
		{`Frank Lloyd Wright" by Brizo®,`, "Brizo", ""},
		{`Jason Wu for Brizo”,`, "Brizo", ""},
		{`CHAUTAUQUA`, "Classic Brass", "Chautauqua"},
		{`APRES CABINET 2026`, "Classic Brass", "Apres"},
		{`PRECIS SILGRANIT`, "Blanco", "Precis"},
		{`ELIZABETH`, "Hydro Systems", "Elizabeth"},
		{`Vintera 33 Silgranit`, "Blanco", "Vintera"},
		{`Item Number List Price`, "Chicago Faucets", ""},
		{`Price Description`, "Brizo", ""},
		{`Robern March 2026 Price Guide`, "Robern", ""},
		{`Kitchen Accessories,`, "Brizo", ""},
		{`The Essential” Shower Series,`, "Brizo", ""},
		{`Starck 3 Single Hole Horizontal Spray Wall Mount Bidet`, "Duravit", "Starck 3"},
		{`WASHBASIN : progressive cartridge`, "Aboutwater", ""},
		{`85/86 Series`, "California Faucets", "85/86 Series"},
		{`Model`, "Robern", ""},
		{`M Series Reserve Medicine Cabinets`, "Robern", "M Series"},
		{`Design by Philippe Starck`, "Duravit", ""},
		{`ME by Starck Single Hole Horizontal Spray Floorstanding Bidet Design by Philippe Starck`, "Duravit", "ME by Starck"},
		{`Gooseneck`, "Chicago Faucets", ""},
		{`Wramszs Iwwrbses Lnvzoase`, "Chicago Faucets", ""},
		{`Handiefndex CTRREON Canridge`, "Chicago Faucets", ""},
		{`Pllsse`, "Artelinea USA", ""},
		{`Numerical Listing`, "Brizo", ""},
		{`Best Selling Combinations`, "Baldwin", ""},
		{`Castlron Pg`, "Barclay", ""},
		{`Schlage Collection`, "Baldwin", "Schlage"},
	}
	for _, c := range cases {
		if got := Collection(c.in, c.brand); got != c.want {
			t.Errorf("Collection(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBullets(t *testing.T) {
	skus := map[string]bool{"T67485-PCLHP": true, "T67485-PNLHP": true, "T67485-PNCOLHP": true, "T67485-BNLHP": true}
	lines := []string{
		`T67485-PCLHP 1416.00 + Touch-Clean® nozzles - swipe away`, `T67485-PNLHP 1699.00 mineral deposits`, `T67485-PNCOLHP 1841.00 £ dshiolamstallanon`,
		`T67485-BNLHP 1982.00 + Overall height = 91/8", spout outlet to`, `deck = 6", spout reach = 9"`, `OBSOLETE Q4 2025 + Accommodates up to21/2" deck N`,
		`+ MUST ORDER ROUGH SEPARATELY`, `+ WaterSense® labeled hand shower`, `« Vitreous china + Overflow`, `+ Also available as a wall-hung`,
		`- Rectangular sink made of high quality Duravit ceramic`, `Solid brass construction`, `Polished chrome plated finish`, `MODEL DESCRIPTION / UPGRADE OPTIONS PRICEUSD PRICE CAD`,
		`for more details, see pages 572-574 THT52D2-R 873`, `*Teak wood is a natural product and there can be variation from piece to piece in wood color, brightness and grain pattern.`,
	}
	got := Bullets(lines, skus, "")
	want := []string{
		`Touch-Clean nozzles - swipe away mineral deposits`, `Overall height = 9-1/8", spout outlet to deck = 6", spout reach = 9"`,
		`WaterSense labeled hand shower`, `Vitreous china`, `Also available as a wall-hung`, `Rectangular sink made of high quality Duravit ceramic`,
		`Solid brass construction`, `Polished chrome plated finish`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("bullets:\n got %q\nwant %q", got, want)
	}
}

func TestFinishes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`TS-30-30 Wall Tub Spout polished chrome 335`, "Polished Chrome"},
		{`Matte Gun Metal PVD 27 P5 A104WU $ 2,803.00 C`, "Matte Gun Metal PVD"},
		{`Brushed Stainless steel 27 93 A508WU $ 2,719.00 c`, "Brushed Stainless Steel"},
		{`443224 LIVEN 25 Single, SILGRANIT, Volcano Gray $742 63`, "Volcano Gray"},
		{`2354600041 1faucet hole White Special Order 1240.00`, "White"},
		{`CTRN58-WH-PB Polished brass + Leg tub drain: part #5599`, "Polished Brass"},
		{`Polished chrome plated finish`, "Polished Chrome"},
		{`LFW100-BC-ORB Metal cross w/porcelain button`, ""},
		{`Black Metal Bt Warm Bronze`, "Black Metal|Warm Bronze"},
	}
	for _, c := range cases {
		if got := strings.Join(Finishes(Norm(c.in)), "|"); got != c.want {
			t.Errorf("Finishes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if LineFinish(`Black Metal Bt Warm Bronze`) != "" {
		t.Error("two finishes on a line must not yield a row finish")
	}
}

func TestDims(t *testing.T) {
	cases := []struct{ in, want string }{
		{`91/8"`, `9-1/8"`}, {`235/8"x181/2"`, `23-5/8"|18-1/2"`}, {`2.375" and 2.75"`, `2.375"|2.75"`}, {`1/2" NPT`, `1/2"`}, {`16"W x 20"H x 4"D`, `16"|20"|4"`},
		{`350 mm`, `350 mm`}, {`9.5in`, `9.5"`}, {`available in 2 sizes`, ``}, {`34%"`, ``}, {`2 ¥4"`, ``}, {`1115/16"`, `11-15/16"`}, {`6 1/8" spout`, `6-1/8"`}, {`2-3/8"`, `2-3/8"`},
		{`9130-37, 9130-60"`, ``}, {`1 3/8" - 1 3/4"`, `1-3/8"|1-3/4"`},
	}
	for _, c := range cases {
		var parts []string
		for _, d := range findDims(Norm(c.in)) {
			parts = append(parts, d.String())
		}
		if got := strings.Join(parts, "|"); got != c.want {
			t.Errorf("findDims(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNorm(t *testing.T) {
	cases := []struct{ in, want string }{
		{`max flow rate: |6gpm at 60 psi`, `max flow rate: 16gpm at 60 psi`},
		{`max flow rate: |.8 gpm`, `max flow rate: 1.8 gpm`},
		{`10 gom at 60 psi`, `10 gpm at 60 psi`},
		{`Overall height = 9⅛”`, `Overall height = 9 1/8"`},
	}
	for _, c := range cases {
		if got := Norm(c.in); got != c.want {
			t.Errorf("Norm(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
