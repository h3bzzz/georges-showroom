package pricefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	c := Detect([]string{"Brand Name", "SKU", "UPC", "Description", "List Price", "Your Cost", "Sell Price", "Finish", "Lead Time", "Status"})
	if c.Brand != "Brand Name" || c.SKU != "SKU" || c.UPC != "UPC" || c.List != "List Price" || c.Cost != "Your Cost" || c.Price != "Sell Price" || c.Lead != "Lead Time" || c.Status != "Status" || c.Finish != "Finish" || c.Description != "Description" {
		t.Fatalf("detect: %+v", c)
	}
	// loose headers: list must not swallow the selling price column and vice versa
	c = Detect([]string{"Manufacturer", "Mfg Part Number", "MSRP", "Cost", "Item Description"})
	if c.Brand != "Manufacturer" || c.SKU != "Mfg Part Number" || c.List != "MSRP" || c.Cost != "Cost" || c.Description != "Item Description" || c.Price != "" {
		t.Fatalf("detect loose: %+v", c)
	}
	if !c.Complete() {
		t.Fatal("expected complete")
	}
}

func TestCents(t *testing.T) {
	for in, want := range map[string]int64{"$1,234.50": 123450, "1234.5": 123450, "1,234": 123400, "": 0, "n/a": 0, "0": 0, "12.345": 1235} {
		if got := Cents(in); got != want {
			t.Errorf("Cents(%q)=%d want %d", in, got, want)
		}
	}
	if leadDays("2-3 weeks") != 21 || leadDays("10 days") != 10 || leadDays("") != 0 {
		t.Error("leadDays")
	}
}

func TestCSV(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a-2026-11-08.csv")
	os.WriteFile(p, []byte("\ufeffBrand,SKU,UPC,List Price,Cost,Status\nBrizo,61053-LF-PC,034449757010,\"$1,006.00\",503.00,Active\nBrizo,,x,1,1,\n"), 0o644)
	srcs, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || !srcs[0].Cols.Complete() {
		t.Fatalf("sources: %+v", srcs)
	}
	var rows []Row
	srcs[0].Rows(func(r Row) bool { rows = append(rows, r); return true })
	if len(rows) != 1 || rows[0].ListCents != 100600 || rows[0].CostCents != 50300 || rows[0].UPC != "034449757010" || rows[0].Discontinued {
		t.Fatalf("rows: %+v", rows)
	}
}
