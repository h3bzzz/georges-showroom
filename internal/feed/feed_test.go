package feed

import (
	"fmt"
	"strings"
	"testing"
)

const sample = `<?xml version="1.0"?>
<Root>
	<Product>
		<productID>17370673</productID>
		<brandName>Aptations</brandName>
		<brandCode>ADT</brandCode>
		<sku>211130</sku>
		<altSku></altSku>
		<type>Home Decor</type>
		<group>Mirrors</group>
		<category>Mirrors</category>
		<subCategory>None</subCategory>
		<collection>Mirror Image</collection>
		<mapPrice>244.9000</mapPrice>
		<msrp>300.0000</msrp>
		<imageName>/ADT/211135-THREE-QUARTER.jpg</imageName>
		<productName>Double Arm Wall Mirror</productName>
		<avProp65>Chromium hexavalent compounds</avProp65>
		<avShipLength>13.00</avShipLength>
		<avFlowRateGPM>1.2</avFlowRateGPM>
		<XV_Center_To_Center>8</XV_Center_To_Center>
		<atBullet2>second</atBullet2>
		<atBullet1>first</atBullet1>
		<avAltImagePath1>/ADT/alt1.jpg</avAltImagePath1>
		<atMarketingCopy>Copy &amp; more.</atMarketingCopy>
	</Product>
</Root>`

func TestParse(t *testing.T) {
	var recs []Record
	if err := Parse(strings.NewReader(sample), func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("%d records", len(recs))
	}
	r := recs[0]
	if r["sku"] != "211130" || r["subCategory"] != "" || r["altSku"] != "" || r["atMarketingCopy"] != "Copy & more." {
		t.Errorf("record: %v", r)
	}
	if b := r.Bullets(); len(b) != 2 || b[0] != "first" || b[1] != "second" {
		t.Errorf("bullets %v", b)
	}
	if a := r.AltImages(); len(a) != 1 || a[0] != "/ADT/alt1.jpg" {
		t.Errorf("alt images %v", a)
	}
	at := r.Attrs()
	if at["Prop65"] != "Chromium hexavalent compounds" || at["Flow Rate GPM"] != "1.2" || at["Center To Center"] != "8" || at["Ship Length"] != "13.00" {
		t.Errorf("attrs %v", at)
	}
	if _, leaked := at["Bullet1"]; leaked {
		t.Error("bullets must not be attrs")
	}
	if Cents("244.9000") != 24490 || Cents("") != 0 || NormSKU("745-945137L") != "745945137L" {
		t.Error("helpers")
	}
	if AttrName("avFlowRateGPM") != "Flow Rate GPM" || AttrName("XV_Center_To_Center") != "Center To Center" || AttrName("avNumHoles") != "Num Holes" {
		t.Errorf("AttrName: %q %q %q", AttrName("avFlowRateGPM"), AttrName("XV_Center_To_Center"), AttrName("avNumHoles"))
	}
}

func TestSplitBySkeleton(t *testing.T) {
	var g []feedRow
	for _, design := range []string{"4900.30.3", "4900.30.4", "4900.20.3", "490.30-238", "4500.30.3", "4500.20.3", "3750.30.3", "1250.30.3", "9000.30.8", "3100.30.3", "2250.30.3"} {
		for _, fin := range []string{"AN", "AR", "WM", "DB", "MW"} {
			g = append(g, feedRow{sku: fin + "." + design, name: "Colonial Passage", finish: fin})
		}
	}
	parts := splitBySkeleton(g)
	if len(parts) != 11 {
		t.Fatalf("want 11 designs, got %d", len(parts))
	}
	if len(parts["4900.30.3"]) != 5 {
		t.Errorf("design should hold its 5 finishes: %v", parts)
	}
	// a group whose SKUs share no structure stays whole
	var h []feedRow
	for i := 0; i < 50; i++ {
		h = append(h, feedRow{sku: fmt.Sprintf("X%d", i*7919)})
	}
	if len(splitBySkeleton(h)) != 1 {
		t.Error("unstructured group must not split")
	}
}

func TestClassifyAndAttrs(t *testing.T) {
	room, cat := classify("ADT", "Aptations", feedRow{name: "Double Arm Wall Mirror", category: "Mirrors", typ: "Home Decor", grp: "Mirrors"})
	if room != "bathroom" || cat != "Mirrors" {
		t.Errorf("mirror → %s/%s", room, cat)
	}
	room, cat = classify("ETK", "Emtek", feedRow{name: "Helios Lever with Modern Rosette", category: "Rosettes", typ: "Hardware", grp: "Door"})
	if room != "hardware" || cat != "Levers" {
		t.Errorf("lever → %s/%s", room, cat)
	}
	room, cat = classify("BAR", "Barclay", feedRow{name: "Towel Bar 24\"", category: "Bathroom Accessories", typ: "Plumbing", grp: "Accessories"})
	if room != "bathroom" || cat != "Towel Bars & Rings" {
		t.Errorf("towel bar → %s/%s", room, cat)
	}
	room, cat = classify("ARH", "Americh", feedRow{name: "Beverly 6640 Soaker", category: "Soaking Tubs", typ: "Plumbing", grp: "Tubs"})
	if room != "bathroom" || cat != "Bathtubs" {
		t.Errorf("tub → %s/%s", room, cat)
	}
	_, cat = classify("BLA", "Blanco", feedRow{name: "Precis Super Single Bowl", category: "Bar Sinks", typ: "Plumbing", grp: "Sinks"})
	if cat != "Kitchen Sinks" {
		t.Errorf("bar sink → %s", cat)
	}
	facts, attrs := mapAttrs([]feedRow{{attrs: map[string]string{"Flow Rate GPM": "1.2", "Num Holes": "3", "Ship Weight": "4", "Prop65": "Lead", "Leadtimemin": "5", "Leadtimemax": "10", "Design": "Modern"}}})
	if facts["Flow Rate"]["value"] != "1.2" || facts["Flow Rate"]["unit"] != "gpm" || facts["Faucet Holes"]["value"] != "3" || facts["Lead Time"]["value"] != "5–10" || facts["Design"]["value"] != "Modern" {
		t.Errorf("facts %v", facts)
	}
	if _, ok := attrs["Ship Weight"]; ok || !strings.Contains(attrs["Prop65 Warning"], "Lead") {
		t.Errorf("attrs %v", attrs)
	}
	if groupKey(feedRow{name: "Helios  Lever", collection: "Modern"}) != groupKey(feedRow{name: "helios lever", collection: "MODERN"}) {
		t.Error("group key should ignore case and spacing")
	}
	if groupKey(feedRow{model: "Bal-6315-rls", name: "6315 2.5BS B/B MRTS LCK"}) != groupKey(feedRow{model: "BAL-6315-RLS", name: "6315 2.5BS B/B MRTS LCK Oil Rubbed"}) {
		t.Error("model should group regardless of name")
	}
	if commonName([]feedRow{{name: "Helios Lever Polished Nickel"}, {name: "Helios Lever"}, {name: "Helios Lever Matte Black"}, {name: "Helios Lever"}}) != "Helios Lever" {
		t.Error("commonName")
	}
}
