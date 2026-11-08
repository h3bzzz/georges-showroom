package taxonomy

import (
	"regexp"
	"sort"
	"strings"
)

// Groups maps a merchandised tile or group name to the leaf categories it covers. Retrieval
// is taxonomy-first: a tile and a type word in a query both resolve to categories, and text
// matching is only the fallback for families with no category.
var Groups = map[string][]string{
	"Faucets":            {"Bathroom Sink Faucets", "Tub Fillers", "Bidet Faucets", "Kitchen Faucets", "Bar & Prep Faucets", "Pot Filler Faucets"},
	"Sinks":              {"Bathroom Sinks", "Kitchen Sinks"},
	"Bathtubs":           {"Bathtubs"},
	"Showers":            {"Shower Heads", "Hand Showers", "Shower Systems", "Shower Arms", "Body Sprays", "Hand Shower Slide Bars", "Shower Faucet Trims", "Thermostatic Valve Trim", "Pressure Balance Valve Trims", "Volume Controls"},
	"Toilets":            {"Toilets", "Bidets", "Urinals"},
	"Vanities":           {"Vanities"},
	"Mirrors":            {"Mirrors"},
	"Medicine Cabinets":  {"Medicine Cabinets"},
	"Cabinet Hardware":   {"Cabinet Pulls", "Knobs", "Hinges"},
	"Door Hardware":      {"Levers", "Handle Sets", "Deadbolts & Locks", "Door Hardware", "Door Stops & Bumpers", "Hinges"},
	"Accessories":        {"Towel Bars & Rings", "Paper Holders", "Grab Bars", "Bath Accessories", "Towel Warmers", "Kitchen Accessories"},
	"Kitchen Faucets":    {"Kitchen Faucets"},
	"Kitchen Sinks":      {"Kitchen Sinks"},
	"Pot Fillers":        {"Pot Filler Faucets"},
	"Bar & Prep Faucets": {"Bar & Prep Faucets"},
	"Filtration":         {"Water Filtration"},
	"Cabinet Knobs":      {"Knobs"},
	"Cabinet Pulls":      {"Cabinet Pulls"},
	"Door Levers":        {"Levers"},
	"Door Knobs":         {"Knobs"},
	"Entry Sets":         {"Handle Sets"},
	"Hinges":             {"Hinges"},
	"Bath Hardware":      {"Towel Bars & Rings", "Paper Holders", "Grab Bars", "Bath Accessories"},
	"Locks & Deadbolts":  {"Deadbolts & Locks"},
	"Ranges":             {"Ranges"},
	"Cooktops":           {"Cooktops"},
	"Ovens":              {"Wall Ovens"},
	"Refrigeration":      {"Refrigerators"},
	"Dishwashers":        {"Dishwashers"},
	"Ventilation":        {"Range Hoods"},
	"Coffee":             {"Coffee Systems"},
	"Laundry":            {"Laundry"},
	"Parts":              {"Faucet Parts", "Rough-In Valves", "Bathtub Parts"},
}

// GroupsFor lists the groups a leaf category belongs to (sorted), for search_docs.groups.
func GroupsFor(category string) []string {
	var out []string
	for g, leaves := range Groups {
		for _, l := range leaves {
			if l == category {
				out = append(out, g)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// Resolve turns a tile/group name or a leaf category into the categories to retrieve.
func Resolve(name string) []string {
	if g, ok := Groups[name]; ok {
		return g
	}
	return []string{name}
}

type categoryRule struct {
	Room string
	Name string
	Re   *regexp.Regexp
}

func rule(room, name, re string) categoryRule {
	return categoryRule{room, name, regexp.MustCompile(`(?i)` + re)}
}

// categoryRules are ordered most specific first; the first match wins.
var categoryRules = []categoryRule{
	rule("", "Rough-In Valves", `\brough(-| )?in\b|\brough\b|valve body|\bvalve only`),
	rule("", "Faucet Parts", `cartridge|aerator|\bstems?\b|escutcheon|repair kit|spare part|replacement|\bflange\b|\bhoses?\b|adapter|extension kit|service part|handle kit|\bparts?\b|mounting kit|supply kit`),
	rule("", "Towel Bars & Rings", `towel|robe hook|bath hook`),
	rule("", "Paper Holders", `paper holder|tissue|toilet paper|roll holder`),
	rule("", "Grab Bars", `grab bar|safety bar|assist bar`),
	rule("", "Bath Accessories", `toilet brush|bowl brush|wc brush|soap dish|soap dispenser|tumbler|\bshelf\b|shelves|accessor|toothbrush|glass holder|tray`),
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
	rule("", "Cabinet Pulls", `\bpulls?\b`),
	rule("", "Knobs", `\bknobs?\b`),
	rule("", "Levers", `\blevers?\b`),
	rule("", "Hinges", `hinge`),
	rule("", "Lighting", `\blight(ing)?\b|sconce|pendant|lamp`),
}

// skuPrefixRules name a category from the model number alone, per brand: a Brizo HLK175 is a
// handle kit however the page around it is titled.
var skuPrefixRules = map[string][]struct {
	Re   *regexp.Regexp
	Name string
}{
	"BRZ": {
		{regexp.MustCompile(`^(HL|HLK|HX|HW|HK|HI|HH)\d`), "Faucet Parts"},
		{regexp.MustCompile(`^RP\d`), "Faucet Parts"},
		{regexp.MustCompile(`^R\d`), "Rough-In Valves"},
		{regexp.MustCompile(`^T\d`), "Shower Faucet Trims"},
	},
	"ROB": {{regexp.MustCompile(`^(MC|PLM|RC|CC)\d`), "Medicine Cabinets"}},
	"*":   {{regexp.MustCompile(`^RP\d`), "Faucet Parts"}},
}

var fixtureCategory = map[string]bool{"Toilets": true, "Bidets": true, "Bathtubs": true, "Bathroom Sinks": true, "Kitchen Sinks": true,
	"Bathroom Sink Faucets": true, "Kitchen Faucets": true, "Tub Fillers": true, "Vanities": true, "Shower Heads": true}
var reBathAccessory = regexp.MustCompile(`(?i)toilet|towel|robe|tissue|\bbath\b|soap|tumbler`)

// CategoryFor returns the merchandised leaf category for a family ("" when none applies).
func CategoryFor(brandCode, sku, room, title, productType string) string {
	u := strings.ToUpper(sku)
	for _, code := range []string{brandCode, "*"} {
		for _, r := range skuPrefixRules[code] {
			if u != "" && r.Re.MatchString(u) {
				return r.Name
			}
		}
	}
	text := title + " " + productType
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

// PartCategories are categories whose families never borrow a collection photograph: a drain
// is not a sink and a cartridge is not a faucet.
var PartCategories = map[string]bool{"Faucet Parts": true, "Bathtub Parts": true, "Drains": true, "Garbage Disposals": true,
	"Rough-In Valves": true, "Volume Controls": true, "Shower Arms": true, "Kitchen Accessories": true, "Water Filtration": true}

// subjectCategories says which categories a picture subject may illustrate.
var subjectCategories = map[string][]string{
	"faucet":     {"Bathroom Sink Faucets", "Tub Fillers", "Bidet Faucets", "Kitchen Faucets", "Bar & Prep Faucets", "Pot Filler Faucets", "Roman Tub Faucets With Hand Showers", "Water Filtration", "Shower Systems", "Faucet Parts", "Single Hole", "Widespread", "Wall Mounted", "Deck Mount", "Bar Mount"},
	"handle":     {"Faucet Parts", "Bathtub Parts", "Levers", "Knobs", "Handles", "Cabinet Pulls", "Handle Sets", "Door Hardware"},
	"trim":       {"Shower Faucet Trims", "Thermostatic Valve Trim", "Pressure Balance Valve Trims", "Pressure Balance Trims With Integrated Diverter", "Volume Controls", "Shower Systems", "Faucet Parts", "Rough-In Valves", "Bathtub Parts"},
	"valve":      {"Rough-In Valves", "Volume Controls", "Faucet Parts", "Bathtub Parts", "Shower Systems", "Water Filtration"},
	"shower":     {"Shower Heads", "Hand Showers", "Shower Systems", "Shower Arms", "Body Sprays", "Hand Shower Slide Bars", "Shower Faucet Trims", "Tub Fillers", "Roman Tub Faucets With Hand Showers"},
	"sink":       {"Bathroom Sinks", "Kitchen Sinks", "Vanities"},
	"tub":        {"Bathtubs"},
	"toilet":     {"Toilets", "Bidets", "Urinals"},
	"part":       {"Faucet Parts", "Rough-In Valves", "Bathtub Parts", "Drains", "Garbage Disposals", "Water Filtration", "Shower Arms", "Kitchen Accessories", "Lighting"},
	"accessory":  {"Towel Bars & Rings", "Paper Holders", "Grab Bars", "Bath Accessories", "Towel Warmers", "Kitchen Accessories", "Water Filtration", "Mirrors", "Lighting"},
	"cabinet":    {"Knobs", "Knob", "Cabinet Pulls", "Handles", "Hinges", "Bath Accessories", "Door Hardware"},
	"hardware":   {"Knobs", "Knob", "Cabinet Pulls", "Handles", "Hinges", "Bath Accessories", "Door Hardware", "Levers", "Handle Sets", "Deadbolts & Locks", "Dummy", "Sets", "Passage", "Privacy", "Keyed", "Single Cylinder", "Door Stops & Bumpers"},
	"doorware":   {"Levers", "Knobs", "Knob", "Handles", "Handle Sets", "Deadbolts & Locks", "Hinges", "Door Hardware", "Dummy", "Sets", "Passage", "Privacy", "Keyed", "Single Cylinder", "Door Stops & Bumpers"},
	"appliance":  {"Ranges", "Cooktops", "Wall Ovens", "Refrigerators", "Dishwashers", "Range Hoods", "Coffee Systems", "Laundry", "Microwaves", "Wine Storage"},
	"vanity":     {"Vanities", "Medicine Cabinets", "Mirrors", "Bathroom Sinks"},
	"mirror":     {"Mirrors", "Medicine Cabinets", "Vanities"},
	"lighting":   {"Lighting", "Mirrors", "Medicine Cabinets", "Bath Accessories"},
	"electrical": {"Door Hardware", "Lighting", "Bath Accessories"},
}

// SubjectFits reports whether a picture of subject may illustrate a family in category. An
// unknown subject fits anything; an uncategorised family accepts anything but non-products.
func SubjectFits(subject, category string) bool {
	if subject == "" {
		return true
	}
	if category == "" {
		return subject != "other"
	}
	for _, c := range subjectCategories[subject] {
		if c == category {
			return true
		}
	}
	return false
}
