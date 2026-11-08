package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"georges/internal/taxonomy"
)

// Applier derives families and items from feed_products. A feed SKU that already exists as
// an item enriches its family (copy, bullets, facts, finishes, category when missing); a SKU
// the site lacks joins the family of its siblings (same brand, product name and collection)
// or starts a new one. Prices: list = MSRP, cost = dealer net; the published price comes from
// the price file (`prices ingest`), which runs after this.
type Applier struct {
	Pool   *pgxpool.Pool
	Brand  string
	DryRun bool
}

// ApplyStats summarise one run.
type ApplyStats struct {
	Rows, Matched, NewItems, NewFamilies, Enriched int
}

type feedRow struct {
	id                                   int64
	sku, skuNorm, upc, name, collection  string
	category, subCategory, grp, typ      string
	finish, finishGroup, desc, marketing string
	bullets                              []string
	msrp, mapc, dn                       int64
	attrs                                map[string]string
	spec, install                        string
}

type item struct {
	id       int64
	familyID int64
}

var reNonWord = regexp.MustCompile(`[^a-z0-9]+`)

func groupKey(r feedRow) string {
	n := strings.TrimSpace(strings.ToLower(r.name))
	if n == "" {
		n = "sku:" + r.skuNorm
	}
	return reNonWord.ReplaceAllString(n, " ") + "|" + reNonWord.ReplaceAllString(strings.ToLower(r.collection), " ")
}

// Run applies every brand (or one).
func (a *Applier) Run(ctx context.Context) (*ApplyStats, error) {
	st := &ApplyStats{}
	rows, err := a.Pool.Query(ctx, `SELECT DISTINCT brand_code FROM feed_products WHERE $1='' OR brand_code=$1 ORDER BY 1`, a.Brand)
	if err != nil {
		return nil, err
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, code := range codes {
		t := time.Now()
		if err := a.applyBrand(ctx, code, st); err != nil {
			return st, fmt.Errorf("%s: %w", code, err)
		}
		slog.Info("feed applied", "brand", code, "took", time.Since(t).Round(time.Millisecond), "new_items", st.NewItems, "new_families", st.NewFamilies)
	}
	return st, nil
}

func (a *Applier) applyBrand(ctx context.Context, code string, st *ApplyStats) error {
	// the brand row (created from the feed when the catalog pipeline never saw the brand)
	var brandID int
	var brandName string
	err := a.Pool.QueryRow(ctx, `SELECT id, name FROM brands WHERE code=$1`, code).Scan(&brandID, &brandName)
	if err == pgx.ErrNoRows {
		if err := a.Pool.QueryRow(ctx, `SELECT brand_name FROM feed_products WHERE brand_code=$1 LIMIT 1`, code).Scan(&brandName); err != nil {
			return err
		}
		if a.DryRun {
			brandID = -1
		} else {
			slug := strings.Trim(reNonWord.ReplaceAllString(strings.ToLower(brandName), "-"), "-")
			room := taxonomy.BrandRooms[strings.ToLower(brandName)]
			if err := a.Pool.QueryRow(ctx, `
				INSERT INTO brands (code, slug, name, room) VALUES ($1,$2,$3,NULLIF($4,''))
				ON CONFLICT (slug) DO UPDATE SET code=COALESCE(brands.code, EXCLUDED.code) RETURNING id`, code, slug, brandName, room).Scan(&brandID); err != nil {
				return err
			}
		}
	} else if err != nil {
		return err
	}
	// existing items of the brand
	existing := map[string]item{}
	ir, err := a.Pool.Query(ctx, `SELECT id, family_id, sku_norm FROM items WHERE brand_id=$1`, brandID)
	if err != nil {
		return err
	}
	for ir.Next() {
		var it item
		var n string
		if err := ir.Scan(&it.id, &it.familyID, &n); err != nil {
			ir.Close()
			return err
		}
		existing[n] = it
	}
	ir.Close()
	// feed rows grouped into families
	fr, err := a.Pool.Query(ctx, `
		SELECT product_id, sku, sku_norm, COALESCE(upc,''), COALESCE(product_name,''), COALESCE(collection,''), COALESCE(category,''), COALESCE(sub_category,''),
		       COALESCE(grp,''), COALESCE(type,''), COALESCE(finish,''), COALESCE(finish_group,''), COALESCE(description,''), COALESCE(marketing,''), bullets,
		       COALESCE(msrp_cents,0), COALESCE(map_cents,0), COALESCE(dn_cents,0), attrs, COALESCE(spec_link,''), COALESCE(install_link,'')
		FROM feed_products WHERE brand_code=$1 ORDER BY product_id`, code)
	if err != nil {
		return err
	}
	groups := map[string][]feedRow{}
	var order []string
	for fr.Next() {
		var r feedRow
		var attrs []byte
		if err := fr.Scan(&r.id, &r.sku, &r.skuNorm, &r.upc, &r.name, &r.collection, &r.category, &r.subCategory, &r.grp, &r.typ, &r.finish, &r.finishGroup,
			&r.desc, &r.marketing, &r.bullets, &r.msrp, &r.mapc, &r.dn, &attrs, &r.spec, &r.install); err != nil {
			fr.Close()
			return err
		}
		_ = json.Unmarshal(attrs, &r.attrs)
		st.Rows++
		k := groupKey(r)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	fr.Close()
	batch := &pgx.Batch{}
	flush := func() error {
		if batch.Len() == 0 || a.DryRun {
			batch = &pgx.Batch{}
			return nil
		}
		br := a.Pool.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return err
			}
		}
		batch = &pgx.Batch{}
		return br.Close()
	}
	for _, k := range order {
		g := groups[k]
		// which existing family do the matched SKUs point at?
		votes := map[int64]int{}
		for _, r := range g {
			if it, ok := existing[r.skuNorm]; ok {
				votes[it.familyID]++
				st.Matched++
			}
		}
		var famID int64
		best := 0
		for id, n := range votes {
			if n > best || n == best && id < famID {
				famID, best = id, n
			}
		}
		lead := g[0]
		title := strings.TrimSpace(lead.name)
		if title == "" {
			title = lead.sku
		}
		room, category := classify(code, brandName, lead)
		facts, attrs := mapAttrs(g)
		finishes := finishList(g)
		if finishes == nil {
			finishes = []string{}
		}
		var bullets []string
		if bullets == nil {
			bullets = []string{}
		}
		for _, r := range g {
			if len(r.bullets) > len(bullets) {
				bullets = r.bullets
			}
		}
		if bullets == nil {
			bullets = []string{}
		}
		desc := lead.desc
		if desc == "" {
			desc = lead.marketing
		}
		desc = clipText(desc, 1200)
		factsJSON, _ := json.Marshal(facts)
		attrsJSON, _ := json.Marshal(attrs)
		if famID == 0 {
			st.NewFamilies++
			if a.DryRun {
				st.NewItems += len(g)
				continue
			}
			tier := taxonomy.TierFor(room, title, lead.typ, category)
			if err := a.Pool.QueryRow(ctx, `
				INSERT INTO families (brand_id, key, title, collection, category, room, tier, description, bullets, attributes, facts, finishes, source, price_min_cents, price_max_cents)
				VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),$9,$10,$11,$12,'feed',NULLIF($13,0),NULLIF($14,0))
				ON CONFLICT (brand_id, key) DO UPDATE SET title=EXCLUDED.title, collection=EXCLUDED.collection, category=COALESCE(families.category, EXCLUDED.category),
				  room=COALESCE(families.room, EXCLUDED.room), description=COALESCE(families.description, EXCLUDED.description),
				  bullets=CASE WHEN cardinality(families.bullets)=0 THEN EXCLUDED.bullets ELSE families.bullets END,
				  attributes=families.attributes || EXCLUDED.attributes, facts=EXCLUDED.facts || families.facts, finishes=EXCLUDED.finishes, updated_at=now()
				RETURNING id`,
				brandID, "feed:"+fmt.Sprint(lead.id), title, lead.collection, category, room, tier, desc, bullets, attrsJSON, factsJSON, finishes, minPrice(g), maxPrice(g)).Scan(&famID); err != nil {
				return err
			}
		} else {
			st.Enriched++
			batch.Queue(`
				UPDATE families SET
				  collection = COALESCE(collection, NULLIF($2,'')),
				  category = COALESCE(category, NULLIF($3,'')),
				  room = COALESCE(room, NULLIF($4,'')),
				  description = COALESCE(description, NULLIF($5,'')),
				  bullets = CASE WHEN cardinality(bullets)=0 THEN $6 ELSE bullets END,
				  attributes = $7::jsonb || attributes,
				  facts = $8::jsonb || facts,
				  finishes = COALESCE((SELECT array_agg(DISTINCT f) FROM unnest(finishes || $9::text[]) f), '{}'),
				  updated_at = now()
				WHERE id=$1`, famID, lead.collection, category, room, desc, bullets, attrsJSON, factsJSON, finishes)
		}
		for _, r := range g {
			if it, ok := existing[r.skuNorm]; ok {
				batch.Queue(`
					UPDATE items SET finish=COALESCE(finish, NULLIF($2,'')), upc=COALESCE(upc, NULLIF($3,'')),
					  list_cents=COALESCE(list_cents, NULLIF($4,0)), cost_cents=COALESCE(cost_cents, NULLIF($5,0)),
					  price_cents=COALESCE(price_cents, NULLIF($4,0))
					WHERE id=$1`, it.id, r.finish, r.upc, r.msrp, r.dn)
				continue
			}
			st.NewItems++
			batch.Queue(`
				INSERT INTO items (family_id, brand_id, sku, sku_norm, finish, upc, list_cents, price_cents, cost_cents, source)
				VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,0),NULLIF($7,0),NULLIF($8,0),'feed')
				ON CONFLICT (brand_id, sku) DO UPDATE SET finish=COALESCE(items.finish, EXCLUDED.finish), upc=COALESCE(items.upc, EXCLUDED.upc),
				  list_cents=COALESCE(items.list_cents, EXCLUDED.list_cents), cost_cents=COALESCE(items.cost_cents, EXCLUDED.cost_cents)`,
				famID, brandID, r.sku, r.skuNorm, r.finish, r.upc, r.msrp, r.dn)
		}
		// spec and install sheets the feed points at
		for _, r := range g {
			if r.spec != "" {
				batch.Queue(`INSERT INTO documents (family_id, kind, url) VALUES ($1,'spec',$2) ON CONFLICT (url) DO NOTHING`, famID, r.spec)
			}
			if r.install != "" {
				batch.Queue(`INSERT INTO documents (family_id, kind, url) VALUES ($1,'install',$2) ON CONFLICT (url) DO NOTHING`, famID, r.install)
			}
		}
		if batch.Len() >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// categoryMap turns Unilog category names into the site's leaves where they differ.
var categoryMap = map[string]string{
	"Bathroom Accessories": "Bath Accessories", "Shower Accessories": "Bath Accessories", "Pulls": "Cabinet Pulls", "Appliance Pulls": "Cabinet Pulls",
	"Backplates": "Door Hardware", "Rosettes": "Door Hardware", "Switch Plates": "Door Hardware", "Air Whirlpool Combo": "Bathtubs", "Soaking Tubs": "Bathtubs",
	"Air Bathtubs": "Bathtubs", "Whirlpool Bathtubs": "Bathtubs", "Bar Sinks": "Kitchen Sinks", "Tub Spouts": "Tub Fillers", "Faucet Rough In Valves": "Rough-In Valves",
	"Shower Parts": "Faucet Parts", "Shower Components": "Faucet Parts", "Escutcheons": "Faucet Parts", "Sink Parts": "Drains", "Drain Assemblies": "Drains",
	"Floor Drains": "Drains", "Tub And Shower Faucets": "Shower Faucet Trims", "Lighting Controls": "Lighting", "Bathroom Furniture": "Vanities", "Toilet Parts": "Faucet Parts",
	"Cabinet Knobs": "Knobs", "Door Knobs": "Knobs", "Door Levers": "Levers", "Entry Sets": "Handle Sets", "Deadbolts": "Deadbolts & Locks", "Towel Bars": "Towel Bars & Rings",
	"Towel Rings": "Towel Bars & Rings", "Robe Hooks": "Towel Bars & Rings", "Toilet Paper Holders": "Paper Holders", "Pot Fillers": "Pot Filler Faucets", "Bar Faucets": "Bar & Prep Faucets",
	"Prep Faucets": "Bar & Prep Faucets", "Shower Doors": "Shower Doors", "Steam Showers": "Shower Systems", "Vessel Sinks": "Bathroom Sinks", "Undermount Sinks": "Bathroom Sinks",
	"Pedestal Sinks": "Bathroom Sinks", "Wall Mount Sinks": "Bathroom Sinks", "Console Sinks": "Bathroom Sinks", "Kitchen Sink Accessories": "Kitchen Accessories",
	"Garbage Disposers": "Garbage Disposals", "Water Filters": "Water Filtration", "Hot Water Dispensers": "Water Filtration", "Thermostatic Valves": "Rough-In Valves",
	"Pressure Balance Valves": "Rough-In Valves", "Diverters": "Volume Controls", "Diverter Valves": "Rough-In Valves", "Bathroom Vanities": "Vanities", "Vanity Tops": "Vanities",
	"Range Hoods": "Range Hoods", "Wall Ovens": "Wall Ovens", "Refrigerators": "Refrigerators", "Freezers": "Refrigerators", "Wine Coolers": "Wine Storage", "Beverage Centers": "Wine Storage",
	"Washers": "Laundry", "Dryers": "Laundry", "Coffee Makers": "Coffee Systems", "Hinges": "Hinges", "Door Stops": "Door Stops & Bumpers",
}

var known = func() map[string]bool {
	m := map[string]bool{}
	for _, leaves := range taxonomy.Groups {
		for _, l := range leaves {
			m[l] = true
		}
	}
	for _, v := range categoryMap {
		m[v] = true
	}
	return m
}()

// classify picks the room and leaf category for a feed group.
func classify(brandCode, brandName string, r feedRow) (room, category string) {
	u := r.category
	if u == "" {
		u = r.subCategory
	}
	if m, ok := categoryMap[u]; ok {
		category = m
	} else if known[u] {
		category = u
	}
	hint := u + " " + r.grp + " " + r.typ
	switch strings.ToLower(r.typ) {
	case "hardware":
		room = "hardware"
	}
	if room == "" {
		room = taxonomy.RoomFor(category, hint, r.name, brandName)
	}
	if room == "" {
		switch strings.ToLower(r.typ) {
		case "plumbing", "home decor", "lighting":
			room = "bathroom"
		}
	}
	if room == "" {
		room = "bathroom"
	}
	// a title-based rule refines a broad feed category (a "Towel Bar" under Bathroom Accessories)
	if t := taxonomy.CategoryFor(brandCode, r.sku, room, r.name, hint); t != "" {
		if category == "" || category == "Bath Accessories" || category == "Door Hardware" || category == "Faucet Parts" || category == "Drains" {
			category = t
		}
	}
	if category == "" && u != "" {
		category = u
	}
	return room, category
}

// factKeys maps feed attribute labels (AttrName) to fact keys and units.
var factKeys = map[string][2]string{
	"Flow Rate GPM": {"Flow Rate", "gpm"}, "Num Holes": {"Faucet Holes", ""}, "Num Handles": {"Handles", ""}, "Mounting": {"Mounting", ""}, "Installation Type": {"Mounting", ""},
	"Material": {"Material", ""}, "Height": {"Height", "in"}, "Width": {"Width", "in"}, "Depth": {"Depth", "in"}, "Length": {"Length", "in"}, "Diameter": {"Diameter", "in"},
	"Overall Height": {"Overall Height", "in"}, "Overall Width": {"Width", "in"}, "Overall Depth": {"Depth", "in"}, "Projection": {"Projection", "in"}, "Extends": {"Spout Reach", "in"},
	"Center Set": {"Centers", "in"}, "Center To Center": {"Centers", "in"}, "Warranty": {"Warranty", ""}, "Certification": {"Certifications", ""}, "Made In USA": {"Made in USA", ""},
	"Country": {"Country of Origin", ""}, "Style List": {"Style", ""}, "Sink Overflow": {"Overflow", ""}, "Num Basins": {"Basins", ""}, "Shape": {"Shape", ""},
	"Lumens": {"Lumens", "lm"}, "Color Temp": {"Color Temperature", "K"}, "Bulb Type": {"Bulb Type", ""}, "Voltage": {"Voltage", "V"}, "Watts": {"Watts", "W"}, "Amps": {"Amps", "A"},
	"Towel Capacity": {"Towel Capacity", ""}, "Time To Heat Minutes": {"Time to Heat", "min"}, "Number Of Bars": {"Bars", ""}, "Plating": {"Finish Process", ""},
	"Design": {"Design", ""}, "Base Width Inches": {"Base Width", "in"}, "Basewidth": {"Base Width", "in"}, "Dimension String": {"Dimensions", ""}, "Base String": {"Base", ""},
	"Man Finish Name": {"Manufacturer Finish Name", ""}, "Line": {"Line", ""}, "Oversized": {"Oversized", ""}, "MTO": {"Made to Order", ""}, "Madetoorder": {"Made to Order", ""},
}

var skipAttrs = map[string]bool{
	"Ship Length": true, "Ship Width": true, "Ship Height": true, "Ship Weight": true, "Ship String": true, "Shipdepth": true, "Package Volume": true, "Packaging Type": true,
	"Packagedimensionsunit": true, "Nmfc": true, "Freight Item": true, "Quantity": true, "Min Order Qty": true, "UOM": true, "Keywords": true, "Mpn": true, "Product Link": true,
	"Web Link": true, "Spec Link": true, "Install Link": true, "Installink": true, "Component": true, "Assemblyrequired": true, "Weight": true, "Alt Image Path": true,
}

// mapAttrs folds the feed attributes of a group into facts (typed, known keys) and attributes
// (the rest, for the specifications table). Lead-time fields become one Lead Time fact.
func mapAttrs(g []feedRow) (map[string]map[string]any, map[string]string) {
	facts := map[string]map[string]any{}
	attrs := map[string]string{}
	var ltMin, ltMax string
	for _, r := range g {
		for k, v := range r.attrs {
			v = strings.TrimSpace(v)
			if v == "" || skipAttrs[k] || strings.HasPrefix(k, "Alt Image") {
				continue
			}
			switch k {
			case "Leadtimemin":
				ltMin = v
				continue
			case "Leadtimemax":
				ltMax = v
				continue
			case "Prop65":
				attrs["Prop65 Warning"] = "This product may expose you to " + v + ", known to the State of California to cause cancer or reproductive harm."
				continue
			}
			if fk, ok := factKeys[k]; ok {
				if _, dup := facts[fk[0]]; !dup {
					f := map[string]any{"value": v, "source": "feed", "conf": 0.9}
					if fk[1] != "" {
						f["unit"] = fk[1]
					}
					facts[fk[0]] = f
				}
				continue
			}
			if _, dup := attrs[k]; !dup && len(attrs) < 40 && len(v) < 200 {
				attrs[k] = v
			}
		}
	}
	if ltMin != "" || ltMax != "" {
		v := ltMin
		if ltMax != "" && ltMax != ltMin {
			if v != "" {
				v += "–"
			}
			v += ltMax
		}
		facts["Lead Time"] = map[string]any{"value": v, "unit": "days", "source": "feed", "conf": 0.8}
	}
	return facts, attrs
}

func finishList(g []feedRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range g {
		f := strings.TrimSpace(r.finish)
		if f == "" || seen[strings.ToLower(f)] {
			continue
		}
		seen[strings.ToLower(f)] = true
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func minPrice(g []feedRow) int64 {
	var m int64
	for _, r := range g {
		if r.msrp > 0 && (m == 0 || r.msrp < m) {
			m = r.msrp
		}
	}
	return m
}

func maxPrice(g []feedRow) int64 {
	var m int64
	for _, r := range g {
		if r.msrp > m {
			m = r.msrp
		}
	}
	return m
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndex(s, ". "); i > n/2 {
		return s[:i+1]
	}
	return s
}
