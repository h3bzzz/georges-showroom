package site

import (
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "html", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestParseProductNativeTrails(t *testing.T) {
	p, err := ParseProduct(open(t, "product_native_trails.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"url":   "https://www.georgesshowroom.com/Pasadena-California/Native-Trails-NSL3619-CX-Charcoal-Drop-In-Bathroom-Sink.HTM",
		"brand": "Native Trails", "sku": "NSL3619-CX", "upc": "816683028879",
		"name": "Trough 3619 in Charcoal - No Faucet Holes", "finish": "Charcoal",
		"collection": "NativeStone Bathroom Sinks", "type": "Drop In Bathroom Sink",
	}
	got := map[string]string{"url": p.URL, "brand": p.Brand, "sku": p.SKU, "upc": p.UPC, "name": p.Name, "finish": p.Finish, "collection": p.Collection, "type": p.ProductType}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if p.OnlineProductID != 9910803 {
		t.Errorf("online id = %d", p.OnlineProductID)
	}
	if p.PriceCents != 112350 || p.MSRPCents != 149800 || p.SavePct != 25 {
		t.Errorf("price=%d msrp=%d save=%d", p.PriceCents, p.MSRPCents, p.SavePct)
	}
	if len(p.Breadcrumbs) != 3 || p.Breadcrumbs[2].Text != "Drop In" {
		t.Errorf("breadcrumbs = %+v", p.Breadcrumbs)
	}
	if len(p.Images) != 4 {
		t.Errorf("images = %v", p.Images)
	}
	if len(p.FinishOptions) != 5 || p.FinishOptions[0].SKU != "NSL3619-AX" || p.FinishOptions[0].Finish != "Ash" || p.FinishOptions[0].PriceCents != 112350 {
		t.Errorf("finish options = %+v", p.FinishOptions)
	}
	if p.FinishCount != 5 {
		t.Errorf("finish count = %d", p.FinishCount)
	}
	for _, k := range []string{"Length", "Width", "Height", "Weight", "Mounting", "ADA", "Material", "Country of Origin", "Shipping"} {
		if p.Attributes[k] == "" {
			t.Errorf("attribute %s missing: %v", k, p.Attributes)
		}
	}
	if p.Attributes["Material"] != "NativeStone Concrete" {
		t.Errorf("material = %q", p.Attributes["Material"])
	}
	if len(p.CollectionItems) == 0 || len(p.Related) == 0 {
		t.Errorf("collection=%d related=%d", len(p.CollectionItems), len(p.Related))
	}
	if p.Related[0].SKU != "AC07DIBLK" {
		t.Errorf("related[0] = %+v", p.Related[0])
	}
	if p.QuoteID != "88073065" {
		t.Errorf("quote id = %q", p.QuoteID)
	}
	if len(p.Bullets) == 0 {
		t.Error("no bullets")
	}
}

func TestParseProductNewportBrass(t *testing.T) {
	p, err := ParseProduct(open(t, "product_newport_brass.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Brand != "Newport Brass" || p.SKU == "" || p.PriceCents == 0 || p.OnlineProductID == 0 {
		t.Errorf("%+v", p)
	}
}

func TestParseListingMaidstone(t *testing.T) {
	l, err := ParseListing(open(t, "listing_maidstone_tubs.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Title != "Maidstone Tubs" || l.Page != 1 || l.TotalPages != 35 {
		t.Errorf("title=%q page=%d/%d", l.Title, l.Page, l.TotalPages)
	}
	if l.NextURL != "https://www.georgesshowroom.com/Maidstone-Tubs-v13.htm?&page=2" {
		t.Errorf("next = %q", l.NextURL)
	}
	if len(l.Items) != 24 {
		t.Fatalf("items = %d", len(l.Items))
	}
	it := l.Items[0]
	if it.Brand != "Maidstone" || it.SKU != "TRAN71DS-CLR" || it.Title != "KIA Freestanding Tub" || it.PriceMinCents != 587500 || it.FinishCount != 4 || it.FamilyKey != "MAS-TRAN71DS" || it.Category != "Free Standing Soaking Tubs" {
		t.Errorf("item0 = %+v", it)
	}
	if len(l.Selections) != 2 || l.Selections[0].Label != "Brand: Maidstone" {
		t.Errorf("selections = %+v", l.Selections)
	}
	names := map[string]int{}
	for _, f := range l.Facets {
		names[f.Name] = len(f.Values)
	}
	if names["Color"] == 0 || names["Collection"] == 0 {
		t.Errorf("facets = %v", names)
	}
	var black FacetValue
	for _, f := range l.Facets {
		if f.Name == "Color" {
			black = f.Values[0]
		}
	}
	if black.Label != "Black" || black.Count != 324 || black.URL != "https://www.georgesshowroom.com/Maidstone-Tubs-v13.htm?&finishStandard=Black" {
		t.Errorf("black = %+v", black)
	}
	if len(l.Sorts) < 2 {
		t.Errorf("sorts = %+v", l.Sorts)
	}
}

func TestParseListingFarmhouse(t *testing.T) {
	l, err := ParseListing(open(t, "listing_farmhouse_sinks.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) == 0 || l.TotalPages == 0 {
		t.Errorf("items=%d pages=%d", len(l.Items), l.TotalPages)
	}
}

func TestParseBrandPage(t *testing.T) {
	l, err := ParseListing(open(t, "brand_westbrass.html"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) == 0 {
		t.Error("no items on brand page")
	}
}
