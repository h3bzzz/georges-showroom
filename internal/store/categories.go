package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Merchandised categories for catalog-derived families. The old site only categorised the
// families it had pages for (1.2k, mostly Brizo and door hardware), so the sidebar counted
// Brizo and nothing else. Every catalog family gets one category from its title and product
// type; rules are ordered most specific first and the first match wins. The names reuse the
// site's taxonomy where it has one so both sources land on the same facet.
type categoryRule struct {
	Room string // "" = any
	Name string
	Re   *regexp.Regexp
}

func rule(room, name, re string) categoryRule {
	return categoryRule{room, name, regexp.MustCompile(`(?i)` + re)}
}

var categoryRules = []categoryRule{
	// parts first: a "shower arm" title that says "rough" is a rough, not an arm
	rule("", "Rough-In Valves", `\brough(-| )?in\b|\brough\b|valve body|\bvalve only`),
	rule("", "Faucet Parts", `cartridge|aerator|\bstems?\b|escutcheon|repair kit|spare part|replacement|\bflange\b|\bhoses?\b|adapter|extension kit|service part|handle kit|\bparts?\b|mounting kit|supply kit`),
	// accessories before fixtures: "toilet paper holder" is not a toilet, "bath hook" not a bath
	rule("", "Towel Bars & Rings", `towel|robe hook|bath hook`),
	rule("", "Paper Holders", `paper holder|tissue|toilet paper|roll holder`),
	rule("", "Grab Bars", `grab bar|safety bar|assist bar`),
	rule("", "Bath Accessories", `soap dish|soap dispenser|tumbler|\bshelf\b|shelves|accessor|toothbrush|glass holder|tray`),
	rule("", "Bathtub Parts", `faucet deck|tub deck|tub skirt|tub apron|tub drain|tub waste|bath deck|tub (feet|legs)|\bskirt\b|\briser\b|supply (lines?|pipes?)`),
	rule("", "Water Filtration", `filter|filtration|everhot|instant hot|hot water (tank|dispenser)|water dispenser|chiller|faucet,? tank|tank,? (cord|3')|cord (and|&) tubing`),
	rule("", "Garbage Disposals", `disposal|disposer`),
	rule("", "Kitchen Accessories", `colander|cutting board|bottom grid|sink grid|basket strainer|soap lotion|air switch|drain board|rinsing basket`),
	rule("", "Shower Arms", `shower arm`),
	rule("", "Hand Shower Slide Bars", `slide ?bar|slidebar|wall bar`),
	rule("", "Hand Showers", `hand ?shower|hand-?held|handheld|handshower`),
	rule("", "Body Sprays", `body ?spray|body jet`),
	rule("", "Shower Heads", `shower ?head|rain ?head|raincan|rain can|rain shower|showerhead|ceiling-mount(ed)? (rain|shower)`),
	rule("", "Shower Systems", `shower system|shower column|exposed shower|shower set|tub/shower set|shower kit|shower combination`),
	rule("", "Thermostatic Valve Trim", `thermostatic`),
	rule("", "Pressure Balance Valve Trims", `pressure balanc`),
	rule("", "Volume Controls", `volume control|diverter`),
	rule("", "Shower Faucet Trims", `(shower|tub|valve) trim|trim kit|\btrims?\b`),
	rule("", "Tub Fillers", `tub filler|roman tub|tub faucet|tub spout|tub set|tub mixer|bath filler|bath mixer|bath spout`),
	rule("", "Pot Filler Faucets", `pot ?filler`),
	rule("", "Bar & Prep Faucets", `bar faucet|prep faucet|bar/prep|bar sink faucet|bar mixer`),
	rule("", "Kitchen Faucets", `kitchen (faucet|mixer|tap)|pull-?down|pull-?out|sink mixer|kitchen sink faucet|bridge faucet|culinary|professional faucet|semi-pro`),
	rule("", "Bidet Faucets", `bidet (faucet|mixer|set|fitting)`),
	rule("", "Bathroom Sink Faucets", `lavatory (faucet|set|mixer)|\blav\b|basin (mixer|faucet|tap|set)|washbasin (mixer|faucet|tap)|widespread|centerset|single[- ]hole|monobloc|monoblock|bathroom faucet|bath faucet|vessel faucet|wall-?mount(ed)? (faucet|mixer|spout)|\bfaucet\b|\bmixer\b`),
	rule("", "Toilets", `toilet|water closet|\bwc\b|washlet|bidet seat`),
	rule("", "Bidets", `\bbidet\b`),
	rule("", "Urinals", `urinal`),
	rule("", "Kitchen Sinks", `kitchen sink|farm(er|house) sink|apron|bar sink|prep sink|silgranit|workstation|double bowl|single bowl|stainless (steel )?sink|fireclay sink|copper sink`),
	rule("", "Bathroom Sinks", `washbasin|\bbasin\b|lavatory|vessel|pedestal|console (sink|basin)|bathroom sink|undermount|drop-?in sink|\bsink\b`),
	rule("", "Bathtubs", `bathtub|soaking|clawfoot|freestanding (tub|bath)|roll top|\btubs?\b|whirlpool|air bath|\bbath\b`),
	rule("", "Vanities", `vanit|washstand|bath(room)? cabinet|console table`),
	rule("", "Medicine Cabinets", `medicine cabinet|mirror(ed)? cabinet|cabinet with mirror|mirror(ed)? wall cabinet`),
	rule("bathroom", "Medicine Cabinets", `(left|right|reversible)[- ]hinge|mirrored door|cabinet door`),
	rule("", "Mirrors", `mirror|medicine cabinet`),
	rule("", "Drains", `\bdrain\b|\bwaste\b|p-trap|pop-?up|grid strainer|strainer|overflow`),
	rule("", "Towel Warmers", `towel warmer|heated rail|radiator`),
	rule("hardware", "Handle Sets", `handleset|handle set|entry set|entrance set|entry trim`),
	rule("hardware", "Deadbolts & Locks", `deadbolt|\block\b|latch|mortise|cylinder`),
	rule("hardware", "Hinges", `hinge`),
	rule("hardware", "Door Stops & Bumpers", `door stop|doorstop|bumper|floor stop|wall stop`),
	rule("hardware", "Cabinet Pulls", `\bpulls?\b|appliance pull|bar pull|cup pull|finger pull|edge pull`),
	rule("hardware", "Knobs", `\bknobs?\b`),
	rule("hardware", "Levers", `\blevers?\b`),
	rule("hardware", "Door Hardware", `rosette|backplate|escutcheon|\bdoor\b|dummy|passage|privacy`),
	rule("appliances", "Ranges", `\brange\b|stove`),
	rule("appliances", "Cooktops", `cooktop|rangetop|\bhob\b`),
	rule("appliances", "Wall Ovens", `\boven\b`),
	rule("appliances", "Range Hoods", `\bhood\b|ventilation|downdraft`),
	rule("appliances", "Refrigerators", `refrigerat|fridge|freezer|ice maker`),
	rule("appliances", "Dishwashers", `dishwasher`),
	rule("appliances", "Wine Storage", `\bwine\b|beverage center`),
	rule("appliances", "Microwaves", `microwave|speed oven`),
	rule("appliances", "Laundry", `washer|dryer|laundry`),
	rule("appliances", "Coffee Systems", `coffee|espresso`),
	// catch-alls by room, so every family lands on a facet
	rule("", "Cabinet Pulls", `\bpulls?\b`),
	rule("", "Knobs", `\bknobs?\b`),
	rule("", "Levers", `\blevers?\b`),
	rule("", "Hinges", `hinge`),
	rule("", "Lighting", `\blight(ing)?\b|sconce|pendant|lamp`),
}

// skuPrefixRules name a category from the model number alone, per brand: a Brizo HLK175 is a
// handle kit however the page around it is titled. Checked before the title rules.
var skuPrefixRules = map[string][]struct {
	Re   *regexp.Regexp
	Name string
}{
	"BRZ": {
		{regexp.MustCompile(`^(HL|HLK|HX|HW|HK|HI|HH)\d`), "Faucet Parts"}, // handle kits
		{regexp.MustCompile(`^RP\d`), "Faucet Parts"},                      // replacement parts
		{regexp.MustCompile(`^R\d`), "Rough-In Valves"},
		{regexp.MustCompile(`^T\d`), "Shower Faucet Trims"},
	},
	"ROB": {
		{regexp.MustCompile(`^(MC|PLM|RC|CC)\d`), "Medicine Cabinets"}, // M, PL, R3 and Candre series cabinets
	},
	"*": {
		{regexp.MustCompile(`^RP\d`), "Faucet Parts"},
	},
}

var fixtureCategory = map[string]bool{"Toilets": true, "Bidets": true, "Bathtubs": true, "Bathroom Sinks": true, "Kitchen Sinks": true,
	"Bathroom Sink Faucets": true, "Kitchen Faucets": true, "Tub Fillers": true, "Vanities": true, "Shower Heads": true}
var reBathAccessory = regexp.MustCompile(`(?i)toilet|towel|robe|tissue|\bbath\b|soap|tumbler`)

// CategoryFor returns the merchandised category for a family (title, product type, room).
func CategoryFor(room, title, ptype string) string {
	return CategoryForSKU("", "", room, title, ptype)
}

// CategoryForSKU is CategoryFor with the brand's SKU-prefix rules applied first.
func CategoryForSKU(brandCode, sku, room, title, ptype string) string {
	u := strings.ToUpper(sku)
	for _, code := range []string{brandCode, "*"} {
		for _, r := range skuPrefixRules[code] {
			if u != "" && r.Re.MatchString(u) {
				return r.Name
			}
		}
	}
	text := title + " " + ptype
	for _, r := range categoryRules {
		if r.Room != "" && r.Room != room {
			continue
		}
		if r.Re.MatchString(text) {
			if room == "hardware" && fixtureCategory[r.Name] {
				// a door-hardware brand does not sell toilets or tubs: "Single Ended Toilet" is a
				// truncated paper holder, "Bath" a towel bar
				if reBathAccessory.MatchString(text) {
					return "Bath Accessories"
				}
				return ""
			}
			return r.Name
		}
	}
	return ""
}

// AssignCategories sets category_path for catalog families from their titles; with force it
// overwrites earlier assignments (site-crawled families keep the site's taxonomy). Returns rows
// changed. The search blob is refreshed afterwards because categories are part of it.
func (s *Store) AssignCategories(ctx context.Context, force bool) (int, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT f.id, COALESCE(f.room,''), COALESCE(f.title,''), COALESCE(f.product_type,''), COALESCE(f.category_path,'{}'),
		  COALESCE(b.code,''), COALESCE(f.family_key,'')
		FROM product_families f JOIN brands b ON b.id=f.brand_id WHERE f.source='catalog'`)
	if err != nil {
		return 0, err
	}
	type fam struct {
		id                int64
		room, title, ptyp string
		cats              []string
		brand, key        string
	}
	fams, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (fam, error) {
		var f fam
		err := r.Scan(&f.id, &f.room, &f.title, &f.ptyp, &f.cats, &f.brand, &f.key)
		return f, err
	})
	if err != nil {
		return 0, err
	}
	// a brand that is mostly door hardware is judged as hardware even for the few families
	// the room pass filed under bathroom ("Single Ended Toilet" paper holder)
	roomCount := map[string]map[string]int{}
	for _, f := range fams {
		if roomCount[f.brand] == nil {
			roomCount[f.brand] = map[string]int{}
		}
		roomCount[f.brand][f.room]++
		roomCount[f.brand][""]++
	}
	hardwareBrand := map[string]bool{}
	for brand, rc := range roomCount {
		if rc[""] >= 20 && rc["hardware"]*10 >= rc[""]*7 {
			hardwareBrand[brand] = true
		}
	}
	batch := &pgx.Batch{}
	n := 0
	for _, f := range fams {
		if len(f.cats) > 0 && !force {
			continue
		}
		room := f.room
		if hardwareBrand[f.brand] {
			room = "hardware"
		}
		cat := CategoryForSKU(f.brand, f.key, room, f.title, f.ptyp)
		if len(f.cats) == 1 && f.cats[0] == cat || cat == "" && len(f.cats) == 0 {
			continue
		}
		newCats := []string{}
		if cat != "" {
			newCats = []string{cat} // with --force an unmatched family loses its stale category
		}
		batch.Queue(`UPDATE product_families SET category_path=$2 WHERE id=$1`, f.id, newCats)
		n++
		if batch.Len() >= 1000 {
			if err := s.Pool.SendBatch(ctx, batch).Close(); err != nil {
				return n, err
			}
			batch = &pgx.Batch{}
		}
	}
	if batch.Len() > 0 {
		if err := s.Pool.SendBatch(ctx, batch).Close(); err != nil {
			return n, err
		}
	}
	_, err = s.Pool.Exec(ctx, `SELECT refresh_family_blob()`)
	return n, err
}

// categoryPattern turns a facet name into a plural-tolerant regex for titles and types:
// "Shower Arms" → `\mshowers?\s+arms?`, "Knobs" → `\mknobs?`.
func categoryPattern(cat string) string {
	var parts []string
	for _, w := range strings.Fields(strings.ToLower(cat)) {
		w = regexp.QuoteMeta(strings.TrimSuffix(strings.TrimSuffix(w, "s"), "ie"))
		if strings.HasSuffix(strings.ToLower(cat), "ies") {
			parts = append(parts, w+"(y|ies)")
			continue
		}
		parts = append(parts, w+"s?")
	}
	return `\m` + strings.Join(parts, `\s+`)
}
