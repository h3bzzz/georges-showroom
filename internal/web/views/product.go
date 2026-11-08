package views

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"georges/internal/assets"
	"georges/internal/store"
	"georges/internal/taxonomy"
)

func roomName(slug string) string { return taxonomy.RoomName(slug) }

func derefS(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func splitMedia(ms []store.Media) (photos, drawings []store.Media) {
	for _, m := range ms {
		switch m.Role {
		case "card", "gallery", "lifestyle":
			photos = append(photos, m)
		case "drawing":
			drawings = append(drawings, m)
		}
	}
	return
}

func galleryURL(hash string) string { return assets.URL(hash, 960) }

func gallerySrcset(hash string, w int) string { return assets.Srcset(hash, max(w, 960)) }

// mediaTitle says what the i-th photo shows: the finish when the picture is keyed to a SKU,
// else where it came from. Shown under the stage so a shopper knows which finish they see.
func mediaTitle(ms []store.Media, i int) string {
	if i >= len(ms) {
		return ""
	}
	m := ms[i]
	if m.Finish != nil && *m.Finish != "" {
		return "Shown in " + *m.Finish
	}
	switch m.Source {
	case "vendor":
		return "Manufacturer photo"
	case "spec":
		return "From the spec sheet"
	}
	return "From the catalog"
}

func hasSpec(f *store.Family) bool {
	for _, d := range f.Documents {
		if d.Kind == "spec" {
			return true
		}
	}
	return false
}

// specRequest is a mailto the showroom answers with the sheet.
func specRequest(f *store.Family) string {
	sku := ""
	if len(f.Items) > 0 {
		sku = f.Items[0].SKU
	}
	subj := url.QueryEscape("Spec sheet request: " + f.Brand.Name + " " + sku)
	body := url.QueryEscape("Please send the specification sheet for " + f.Brand.Name + " " + f.Title + " (" + sku + ").")
	return "mailto:sales@georgesshowroom.com?subject=" + subj + "&body=" + body
}

func priceRange(items []store.Item) (lo, hi int64) {
	for _, it := range items {
		if it.PriceCents == nil || *it.PriceCents <= 0 || it.PriceFlag != nil {
			continue
		}
		if lo == 0 || *it.PriceCents < lo {
			lo = *it.PriceCents
		}
		if *it.PriceCents > hi {
			hi = *it.PriceCents
		}
	}
	return
}

func finishLabel(it store.Item) string {
	if it.Finish != nil && *it.Finish != "" {
		return *it.Finish
	}
	return "Standard"
}

func toneOf(finish *string) string {
	if finish == nil {
		return ""
	}
	e := taxonomy.ClassifyFinish(*finish)
	if e.Head != "" {
		return taxonomy.ToneGroup(e.Head)
	}
	return ""
}

// specRows merges the site's attribute table with catalog facts; site values win.
func specRows(f *store.Family) [][2]string {
	seen := map[string]bool{}
	var out [][2]string
	keys := make([]string, 0, len(f.Attributes))
	for k := range f.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(f.Attributes[k])
		if v == "" || len(v) > 160 {
			continue
		}
		seen[strings.ToLower(k)] = true
		out = append(out, [2]string{k, v})
	}
	for _, fa := range f.Facts {
		if seen[strings.ToLower(fa.Key)] {
			continue
		}
		v := fa.Value
		if fa.Unit != "" {
			v += " " + fa.Unit
		}
		out = append(out, [2]string{fa.Key, v})
	}
	if len(f.Finishes) > 0 && !seen["finish"] && !seen["finishes"] {
		out = append(out, [2]string{"Finishes", strings.Join(f.Finishes, ", ")})
	}
	return out
}

func docLabel(d store.Document) string {
	kind := map[string]string{"spec": "Specification sheet", "install": "Installation guide", "care": "Care & cleaning", "brochure": "Brochure"}[d.Kind]
	if kind == "" {
		kind = strings.Title(d.Kind)
	}
	if d.Pages != nil && *d.Pages > 0 {
		return fmt.Sprintf("%s (PDF, %d %s)", kind, *d.Pages, Plural(*d.Pages, "page", "pages"))
	}
	return kind + " (PDF)"
}

// uniqueBullets drops repeats that differ only in spacing or case (two catalog years).
func uniqueBullets(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range in {
		k := strings.ToLower(strings.Join(strings.Fields(b), " "))
		k = strings.TrimSuffix(k, " standard")
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, b)
		if len(out) == 8 {
			break
		}
	}
	return out
}

// selectedItem is the finish the stage picture shows (the item whose photo is the card),
// else the first item.
func selectedItem(items []store.Item, photos []store.Media) int {
	if len(photos) == 0 {
		return 0
	}
	for i, it := range items {
		if it.Hash != nil && *it.Hash == photos[0].Hash {
			return i
		}
	}
	if photos[0].ItemID != nil {
		for i, it := range items {
			if it.ID == *photos[0].ItemID {
				return i
			}
		}
	}
	return 0
}
