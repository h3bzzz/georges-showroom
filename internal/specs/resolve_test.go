package specs

import "testing"

func TestResolvers(t *testing.T) {
	cal := map[string]bool{"PC": true, "MBLK": true, "ABF": true}
	c := calFaucets("6502ZBF-PC", "", cal)
	if c[0].URL != "https://images.calfaucets.com/technical/6502ZBF-XX_sp.pdf" || c[1].URL != "https://images.calfaucets.com/technical/6502ZBF_sp.pdf" {
		t.Fatalf("cal %+v", c)
	}
	if chicago("1012-CP", "", nil)[0].URL != "https://www.chicagofaucets.com/sites/default/files/spec-sheets/faucets/1012-CP.pdf" {
		t.Fatal("chicago")
	}
	if newportBrass("8200-5113/26", "", nil)[0].URL != "https://www.newportbrass.com/documents/dynamicdocuments/spec_8200-5113.pdf" {
		t.Fatal("newport")
	}
	if kohler("K-2214-0", "", nil)[0].URL != "https://techcomm.kohler.com/techcomm/pdf/K-2214_spec_US-CA_Kohler_en.pdf" {
		t.Fatal("kohler")
	}
	if baldwin("5101.XXX", "", nil)[0].URL != "https://baldwinhardware.blob.core.windows.net/documents/5101_techdrawing.pdf" {
		t.Fatal("baldwin")
	}
	if l := lacava("16D3.T.S", "", nil); l[0].URL != "https://lacava.com/pdfs/16D3.T.S.pdf" || l[1].URL != "https://lacava.com/pdfs/16D3.pdf" {
		t.Fatalf("lacava %+v", l)
	}
	brz := map[string]bool{"PC": true, "PN": true, "BNX": true}
	b := brizo("62822LF-PC", "Water Filtration", brz)
	if b[0].Name != "BSP-K-62822LF Rev A.pdf" || len(b) != 10 {
		t.Fatalf("brizo %+v", b[0])
	}
	if brizo("61023LF-PNLHP", "Bathroom Sink Faucets", brz)[0].Name != "BSP-L-61023LF-LHP Rev A.pdf" {
		t.Fatalf("brizo glued: %s", brizo("61023LF-PNLHP", "Bathroom Sink Faucets", brz)[0].Name)
	}
}
