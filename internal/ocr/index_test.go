package ocr

import (
	"strings"
	"testing"
)

func TestIsIndexPage(t *testing.T) {
	var words []Word
	// a numerical index: "Model No. Page" header, then SKU / page pairs
	words = append(words, Word{Text: "Numerical", X: 100, Y: 10, W: 80, H: 12}, Word{Text: "Index", X: 200, Y: 10, W: 50, H: 12})
	for i := 0; i < 60; i++ {
		words = append(words, Word{Text: "D4006" + string(rune('0'+i%10)), X: 10, Y: 100 + i*20, W: 60, H: 12},
			Word{Text: "186,193", X: 100, Y: 100 + i*20, W: 60, H: 12})
	}
	if !IsIndexPage(words) {
		t.Error("numerical index not recognised")
	}
	// prices still vanish on an index page even when a token could parse as money
	for _, ln := range Lines(words) {
		if ln.PriceCents != 0 {
			t.Fatalf("index page produced price %d from %q", ln.PriceCents, ln.Text)
		}
	}
	// a price-book page: SKU, description words, price
	var book []Word
	for i := 0; i < 30; i++ {
		y := 100 + i*20
		book = append(book,
			Word{Text: "A008WU", X: 10, Y: y, W: 60, H: 12},
			Word{Text: "Single-control", X: 100, Y: y, W: 90, H: 12},
			Word{Text: "bidet", X: 200, Y: y, W: 40, H: 12},
			Word{Text: "mixer", X: 250, Y: y, W: 40, H: 12},
			Word{Text: "$1,787.00", X: 400, Y: y, W: 60, H: 12})
	}
	if IsIndexPage(book) {
		t.Error("price book mistaken for an index")
	}
	lines := Lines(book)
	if len(lines) == 0 || lines[0].PriceCents != 178700 {
		t.Fatalf("price book line lost its price: %+v", lines)
	}
	// a dense price table: many SKUs, no prose, money everywhere — not an index
	var table []Word
	for i := 0; i < 50; i++ {
		y := 100 + i*20
		table = append(table, Word{Text: "62858LF-PC", X: 10, Y: y, W: 70, H: 12}, Word{Text: "1,234.00", X: 100, Y: y, W: 50, H: 12},
			Word{Text: "62858LF-PN", X: 200, Y: y, W: 70, H: 12}, Word{Text: "1,500.00", X: 290, Y: y, W: 50, H: 12},
			Word{Text: "62858LF-SS", X: 400, Y: y, W: 70, H: 12}, Word{Text: "1,400.00", X: 490, Y: y, W: 50, H: 12})
	}
	if IsIndexPage(table) {
		t.Error("dense price table mistaken for an index")
	}
	if ln := Lines(table); len(ln) == 0 || ln[0].PriceCents == 0 {
		t.Errorf("price table rows lost their prices: %+v", ln[:1])
	}
	// Brizo: "SKU 610.00 description" — the price is SKU-shaped (digits and a dot) but is money
	brizo := []Word{{Text: "81375-PC-2.5", X: 10, Y: 10, W: 80, H: 12}, {Text: "610.00", X: 100, Y: 10, W: 40, H: 12}, {Text: "+", X: 150, Y: 10, W: 5, H: 12}, {Text: "Touch-Clean", X: 160, Y: 10, W: 60, H: 12}}
	if ln := Lines(brizo); len(ln) != 1 || ln[0].PriceCents != 61000 || len(ln[0].SKUs) != 1 {
		t.Errorf("Brizo row: %+v", ln)
	}
	// split dollar sign and bare whole-dollar styles
	split := []Word{{Text: "A008WU", X: 10, Y: 10, W: 60, H: 12}, {Text: "bidet", X: 100, Y: 10, W: 40, H: 12}, {Text: "mixer", X: 150, Y: 10, W: 40, H: 12}, {Text: "$", X: 300, Y: 10, W: 10, H: 12}, {Text: "1,787.00", X: 320, Y: 10, W: 60, H: 12}}
	if ln := Lines(split); len(ln) != 1 || ln[0].PriceCents != 178700 {
		t.Errorf("split dollar sign: %+v", ln)
	}
	bare := []Word{{Text: "7704-1", X: 10, Y: 10, W: 60, H: 12}, {Text: "Single", X: 100, Y: 10, W: 40, H: 12}, {Text: "Hole", X: 150, Y: 10, W: 40, H: 12}, {Text: "Bidet", X: 200, Y: 10, W: 40, H: 12}, {Text: "polished", X: 250, Y: 10, W: 50, H: 12}, {Text: "chrome", X: 310, Y: 10, W: 50, H: 12}, {Text: "1095", X: 400, Y: 10, W: 40, H: 12}}
	if ln := Lines(bare); len(ln) != 1 || ln[0].PriceCents != 109500 {
		t.Errorf("bare dollars after a description: %+v", ln)
	}
	pageRef := []Word{{Text: "THT52-R", X: 10, Y: 10, W: 60, H: 12}, {Text: "693", X: 400, Y: 10, W: 40, H: 12}}
	if ln := Lines(pageRef); len(ln) != 1 || ln[0].PriceCents != 0 {
		t.Errorf("bare number without a description must not be a price: %+v", ln)
	}
	// a page printed with cents: a signed amount with no point has lost it to OCR
	var cents []Word
	for i := 0; i < 4; i++ {
		y := 100 + i*20
		cents = append(cents, Word{Text: "W036U", X: 10, Y: y, W: 60, H: 12}, Word{Text: "Chrome", X: 100, Y: y, W: 50, H: 12}, Word{Text: "9102", X: 160, Y: y, W: 40, H: 12}, Word{Text: "$", X: 300, Y: y, W: 10, H: 12}, Word{Text: "200.00", X: 320, Y: y, W: 50, H: 12})
	}
	cents = append(cents,
		Word{Text: "A713BUCX", X: 10, Y: 200, W: 60, H: 12}, Word{Text: "Brushed", X: 100, Y: 200, W: 50, H: 12}, Word{Text: "$22800", X: 300, Y: 200, W: 50, H: 12},
		Word{Text: "ML6454", X: 10, Y: 220, W: 60, H: 12}, Word{Text: "Mixer", X: 100, Y: 220, W: 50, H: 12}, Word{Text: "$4,93500", X: 300, Y: 220, W: 50, H: 12},
		Word{Text: "AT040", X: 10, Y: 240, W: 60, H: 12}, Word{Text: "Towel", X: 100, Y: 240, W: 50, H: 12}, Word{Text: "Ring", X: 160, Y: 240, W: 50, H: 12}, Word{Text: "$7495", X: 300, Y: 240, W: 50, H: 12}, Word{Text: "$89.95", X: 360, Y: 240, W: 50, H: 12})
	want := map[string]int64{"W036U": 20000, "A713BUCX": 22800, "ML6454": 493500, "AT040": 7495}
	for _, ln := range Lines(cents) {
		if w, ok := want[ln.SKUs[0].Text]; ok && ln.PriceCents != w {
			t.Errorf("cents-style page: %s read %d, want %d (%s)", ln.SKUs[0].Text, ln.PriceCents, w, ln.Text)
		}
	}
	// the same amount on a whole-dollar page (Emtek) is whole dollars
	whole := []Word{{Text: "451211", X: 10, Y: 10, W: 60, H: 12}, {Text: "Single", X: 100, Y: 10, W: 50, H: 12}, {Text: "Cylinder", X: 160, Y: 10, W: 50, H: 12}, {Text: "$1492", X: 300, Y: 10, W: 50, H: 12},
		{Text: "451332", X: 10, Y: 30, W: 60, H: 12}, {Text: "Dummy", X: 100, Y: 30, W: 50, H: 12}, {Text: "$878", X: 300, Y: 30, W: 50, H: 12}}
	if ln := Lines(whole); len(ln) != 2 || ln[0].PriceCents != 149200 || ln[1].PriceCents != 87800 {
		t.Errorf("whole-dollar page: %+v", ln)
	}
	// a parenthesised finish code or cross-reference is not the line's SKU, so it takes no price
	ref := []Word{{Text: "5057", X: 10, Y: 10, W: 40, H: 12}, {Text: "Octagon", X: 60, Y: 10, W: 50, H: 12}, {Text: "Knob", X: 120, Y: 10, W: 40, H: 12}, {Text: "Satin", X: 170, Y: 10, W: 40, H: 12}, {Text: "Nickel", X: 220, Y: 10, W: 40, H: 12}, {Text: "(US15)", X: 270, Y: 10, W: 40, H: 12}, {Text: "$240", X: 320, Y: 10, W: 40, H: 12}}
	for _, ln := range Lines(ref) {
		for _, k := range ln.SKUs {
			if strings.Contains(k.Text, "US15") {
				t.Errorf("parenthesised finish code taken as a SKU: %+v", ln)
			}
		}
	}
	// a SKU that is all digits is never the price, and page references are not money
	row := []Word{{Text: "20099", X: 10, Y: 10, W: 40, H: 12}, {Text: "46189_73628", X: 60, Y: 10, W: 80, H: 12}, {Text: "DIAMANTATO", X: 150, Y: 10, W: 80, H: 12}}
	for _, ln := range Lines(row) {
		if ln.PriceCents != 0 {
			t.Errorf("SKU read as price: %d", ln.PriceCents)
		}
	}
}
