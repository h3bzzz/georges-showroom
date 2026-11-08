package media

import "testing"

func TestVerdictReconciles(t *testing.T) {
	cases := []struct {
		c    Candidate
		want string
	}{
		{Candidate{Source: "vendor"}, "photo"},
		{Candidate{PKind: "photo"}, "photo"},
		{Candidate{PKind: "table", Kind: "photo", KindScore: 0.9, Quality: 0.5}, "photo"},
		{Candidate{PKind: "table", Kind: "photo", KindScore: 0.9, Quality: 0.1}, "table"},
		{Candidate{PKind: "photo", Kind: "drawing", KindScore: 0.7}, "drawing"},
		{Candidate{PKind: "photo", Kind: "callout", KindScore: 0.55}, "drawing"},
		{Candidate{PKind: "photo", Kind: "detail", KindScore: 0.8}, "detail"},
		{Candidate{PKind: "photo", Kind: "detail", KindScore: 0.4}, "photo"},
		{Candidate{PKind: "drawing", Kind: "photo", KindScore: 0.95}, "photo"},
	}
	for _, tc := range cases {
		if got := Verdict(tc.c); got != tc.want {
			t.Errorf("%+v: got %s want %s", tc.c, got, tc.want)
		}
	}
}

func TestChooseVendorBeatsCrop(t *testing.T) {
	f := Family{ID: 1, Category: "Kitchen Faucets"}
	crop := Candidate{AssetID: 10, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Width: 400, Height: 500, Quality: 0.6}
	vendor := Candidate{AssetID: 20, FamilyID: 1, How: "vendor", LinkWeight: 1, Source: "vendor", Width: 2000, Height: 2000, Quality: 0.9}
	picks := Choose(f, []Candidate{crop, vendor})
	if picks[0].Role != RoleCard || picks[0].AssetID != 20 || picks[0].Grade != 0 {
		t.Fatalf("card = %+v", picks[0])
	}
	if len(picks) != 2 || picks[1].Role != RoleGallery || picks[1].AssetID != 10 {
		t.Fatalf("gallery = %+v", picks[1:])
	}
}

func TestChooseVetoes(t *testing.T) {
	f := Family{ID: 1, Category: "Kitchen Faucets"}
	handle := Candidate{AssetID: 1, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Subject: "handle", SubjectScore: 0.8, Width: 400, Height: 400, Quality: 0.6}
	swatch := Candidate{AssetID: 2, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "text", KindScore: 0.7, PKind: "photo", Width: 400, Height: 400}
	sliver := Candidate{AssetID: 3, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Width: 100, Height: 900, Quality: 0.6}
	pageOn6 := Candidate{AssetID: 4, FamilyID: 1, How: "page", LinkWeight: 0.3, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Width: 400, Height: 400, PagePhotos: 3, PageModels: 6}
	drawing := Candidate{AssetID: 5, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "drawing", KindScore: 0.8, PKind: "photo", Width: 400, Height: 400}
	picks := Choose(f, []Candidate{handle, swatch, sliver, pageOn6, drawing})
	for _, p := range picks {
		if p.Role == RoleCard {
			t.Fatalf("no card expected, got %+v", p)
		}
	}
	var gal, dr int
	for _, p := range picks {
		switch p.Role {
		case RoleGallery:
			gal++
		case RoleDrawing:
			dr++
		}
	}
	if dr != 1 {
		t.Fatalf("drawings = %d", dr)
	}
	// the sliver and the busy-page photo still reach the gallery; the swatch and the mis-subject handle do not
	if gal != 2 {
		t.Fatalf("gallery = %d, picks %+v", gal, picks)
	}
}

func TestChooseCutNeverCard(t *testing.T) {
	f := Family{ID: 1, Category: "Bathroom Sink Faucets"}
	sliced := Candidate{AssetID: 1, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Width: 400, Height: 500, Quality: 0.7, Cut: 0.5}
	whole := Candidate{AssetID: 2, FamilyID: 1, How: "near", LinkWeight: 0.6, Source: "catalog", Kind: "photo", KindScore: 0.8, PKind: "photo", Width: 300, Height: 300, Quality: 0.4}
	picks := Choose(f, []Candidate{sliced, whole})
	if picks[0].Role != RoleCard || picks[0].AssetID != 2 {
		t.Fatalf("card = %+v", picks[0])
	}
	// a sliced crop is never shown, not even at the back of the gallery, whatever else is wrong with it
	tallSliced := Candidate{AssetID: 4, FamilyID: 1, How: "sku", LinkWeight: 0.95, Source: "catalog", Kind: "photo", KindScore: 0.9, PKind: "photo", Width: 100, Height: 500, Quality: 0.7, Cut: 0.5}
	picks = Choose(f, []Candidate{sliced, whole, tallSliced})
	if len(picks) != 1 {
		t.Fatalf("sliced should not reach the gallery: %+v", picks)
	}
	// a vendor master is never "cut": a tight bounding box touches the frame by design
	master := Candidate{AssetID: 3, FamilyID: 1, How: "vendor", LinkWeight: 1, Source: "vendor", Width: 2800, Height: 900, Quality: 0.8, BgWhite: 0.9, Cut: 0.95}
	picks = Choose(f, []Candidate{sliced, whole, master})
	if picks[0].Role != RoleCard || picks[0].AssetID != 3 {
		t.Fatalf("vendor master should be the card: %+v", picks[0])
	}
}

func TestGrade(t *testing.T) {
	f := Family{Category: "Bathtubs"}
	good := Candidate{How: "sku", Source: "catalog", Kind: "photo", KindScore: 0.8, Subject: "tub", SubjectScore: 0.9, Width: 500, Height: 400}
	if g := Grade(good, f); g != 0 {
		t.Fatalf("good grade %d", g)
	}
	near := Candidate{How: "near", Source: "catalog", Kind: "photo", KindScore: 0.8, Width: 500, Height: 400}
	if g := Grade(near, f); g != 1 {
		t.Fatalf("near grade %d", g)
	}
	detail := Candidate{How: "sku", Source: "catalog", Kind: "detail", KindScore: 0.35, Width: 500, Height: 400}
	if g := Grade(detail, f); g != 2 {
		t.Fatalf("detail grade %d", g)
	}
}

func TestCardPrefersChromeMaster(t *testing.T) {
	f := Family{ID: 1, Category: "Bathroom Sink Faucets"}
	white := Candidate{AssetID: 1, FamilyID: 1, How: "vendor", LinkWeight: 1, Source: "vendor", Width: 2800, Height: 2800, Quality: 0.8, BgWhite: 0.9, Finish: "Matte White"}
	chrome := Candidate{AssetID: 2, FamilyID: 1, How: "vendor", LinkWeight: 1, Source: "vendor", Width: 2800, Height: 2800, Quality: 0.8, BgWhite: 0.9, Finish: "Polished Chrome"}
	picks := Choose(f, []Candidate{white, chrome})
	if picks[0].Role != RoleCard || picks[0].AssetID != 2 {
		t.Fatalf("card = %+v", picks[0])
	}
}
