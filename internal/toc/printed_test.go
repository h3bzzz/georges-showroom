package toc

import "testing"

func TestParsePrinted(t *testing.T) {
	text := "Table of Contents\nKitchen\nDavoli | K10 Series\np.38\nDescanso | K30 Series\np.46\nCulinary | 30/50/51/81 Series\np.90\nBath\nDescanso | 30/31 Series Montecito | 32/33 Series\np. 100 B p. 120 o\nJalama | 39 Series\nNEW p.162\n32 800.822.8855 | californiafaucets.com\n"
	es := ParsePrinted(text)
	if len(es) < 3 {
		t.Fatalf("got %d entries: %+v", len(es), es)
	}
	if es[0].Title != "Davoli · K10 Series" || es[0].Printed != 38 || es[0].Group != "Kitchen" {
		t.Errorf("first = %+v", es[0])
	}
	foundBath := false
	for _, e := range es {
		if e.Group == "Bath" {
			foundBath = true
		}
	}
	if !foundBath {
		t.Errorf("no Bath group: %+v", es)
	}
}
