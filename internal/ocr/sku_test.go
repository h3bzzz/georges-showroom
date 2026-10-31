package ocr

import "testing"

func TestJunkSKU(t *testing.T) {
	junk := []string{
		"1016/CSA", "4.5L/MIN", "8.8L/MIH", "6.6L/MIN", "6.61./MIN", "A112.1016/ASSE", "AL12.1016/ASSE",
		"2012/CSA", "NSF/ANSI372", "0.3MG/L", "1.2L/MIN", "1X/10X", "10X/1X", "2700K/4000K", "KELVIN/4000",
		"AMPS/0.75", "MODEL/COLOR1/COLOR2", "W/6701", "50/BOX", "2.5/T1", "120Z", "12OZ", "RENT/LENT", "LH/RH",
		"6F/7F/8F", "(2700K)", "120V/60HZ", "800-877-2376", "1-800-877-2376",
	}
	for _, s := range junk {
		if !JunkSKU(s) {
			t.Errorf("JunkSKU(%q) = false, want true", s)
		}
		if LooksLikeSKU(s) {
			t.Errorf("LooksLikeSKU(%q) = true for junk", s)
		}
	}
	real := []string{
		"B/3-461WH", "SO30CE/B/TH", "2-628/03N", "MCL3450WS/S", "A9320-8-CHP/MB", "99/00A.SD1", "HPA.5436",
		"62858LF-PC", "A008WU", "85350.XXX.FD", "SH-162-12.25", "37231", "RP71650NK", "K30-123-XX", "451211",
		"UC30276RP3E4N", "AU-24K", "US10B",
	}
	for _, s := range real {
		if JunkSKU(s) {
			t.Errorf("JunkSKU(%q) = true, want false", s)
		}
	}
}

func TestIsReference(t *testing.T) {
	for _, s := range []string{"(US10B)", "(85354),", "(2700K)", "(600-CP)"} {
		if !IsReference(s) {
			t.Errorf("IsReference(%q) = false", s)
		}
	}
	for _, s := range []string{"US10B", "85354", "(R)17178", "A008WU)", "()"} {
		if IsReference(s) {
			t.Errorf("IsReference(%q) = true", s)
		}
	}
}
