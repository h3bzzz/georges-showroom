package store

import "strings"

// Taxonomy-first retrieval. A merchandised tile ("Faucets") and a product-type word in a
// query ("faucet") both resolve to a set of categories; families are retrieved by category
// membership first and by title text only as a fallback for families that have no category.
// This is how large catalogs avoid "tub for use with wall-mounted faucets" answering "faucet":
// the category is a curated field, the title is noisy OCR.

// CategoryGroups maps a tile or group name to the leaf categories it covers.
var CategoryGroups = map[string][]string{
	"Faucets":            {"Bathroom Sink Faucets", "Tub Fillers", "Bidet Faucets", "Kitchen Faucets", "Bar & Prep Faucets", "Pot Filler Faucets"},
	"Sinks":              {"Bathroom Sinks", "Kitchen Sinks"},
	"Bathtubs":           {"Bathtubs"},
	"Showers":            {"Shower Heads", "Hand Showers", "Shower Systems", "Shower Arms", "Body Sprays", "Hand Shower Slide Bars", "Shower Faucet Trims", "Thermostatic Valve Trim", "Pressure Balance Valve Trims", "Volume Controls"},
	"Toilets":            {"Toilets", "Bidets", "Urinals"},
	"Vanities":           {"Vanities"},
	"Mirrors":            {"Mirrors"},
	"Medicine Cabinets":  {"Medicine Cabinets"},
	"Cabinet Hardware":   {"Cabinet Pulls", "Knobs", "Hinges"},
	"Door Hardware":      {"Levers", "Handle Sets", "Deadbolts & Locks", "Door Hardware", "Door Stops & Bumpers", "Dummy", "Sets", "Passage", "Privacy", "Hinges"},
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

// typeGroups maps a query type word (keys of typeSynonyms) to categories.
var typeGroups = map[string][]string{
	"faucet": CategoryGroups["Faucets"], "tap": CategoryGroups["Faucets"], "mixer": CategoryGroups["Faucets"],
	"sink": CategoryGroups["Sinks"], "basin": {"Bathroom Sinks"}, "lavatory": {"Bathroom Sinks", "Bathroom Sink Faucets"},
	"toilet": {"Toilets"}, "bidet": {"Bidets", "Bidet Faucets"}, "urinal": {"Urinals"},
	"tub": {"Bathtubs", "Tub Fillers"}, "bathtub": {"Bathtubs"},
	"shower": CategoryGroups["Showers"], "showerhead": {"Shower Heads"}, "handshower": {"Hand Showers"},
	"filler": {"Tub Fillers", "Pot Filler Faucets"}, "potfiller": {"Pot Filler Faucets"},
	"vanity": {"Vanities"}, "cabinet": {"Vanities"}, "mirror": {"Mirrors"},
	"knob": {"Knobs"}, "pull": {"Cabinet Pulls"}, "handle": {"Levers", "Cabinet Pulls", "Handle Sets"}, "lever": {"Levers"},
	"hinge": {"Hinges"}, "lock": {"Deadbolts & Locks"}, "deadbolt": {"Deadbolts & Locks"},
	"towel": {"Towel Bars & Rings", "Towel Warmers"}, "hook": {"Towel Bars & Rings"},
	"drain": {"Drains"}, "disposal": {"Garbage Disposals"}, "filter": {"Water Filtration"}, "dispenser": {"Bath Accessories", "Water Filtration"},
	"range": {"Ranges"}, "stove": {"Ranges"}, "oven": {"Wall Ovens"}, "cooktop": {"Cooktops"}, "hood": {"Range Hoods"},
	"refrigerator": {"Refrigerators"}, "fridge": {"Refrigerators"}, "freezer": {"Refrigerators"}, "dishwasher": {"Dishwashers"},
	"washer": {"Laundry"}, "dryer": {"Laundry"}, "microwave": {"Microwaves"}, "wine": {"Wine Storage"},
	"valve": {"Rough-In Valves", "Volume Controls"}, "trim": {"Shower Faucet Trims", "Thermostatic Valve Trim", "Pressure Balance Valve Trims"},
	"cartridge": {"Faucet Parts"}, "spout": {"Faucet Parts", "Tub Fillers"}, "sprayer": {"Faucet Parts", "Kitchen Faucets"}, "aerator": {"Faucet Parts"},
	"light": {"Lighting"}, "heater": {"Towel Warmers"},
}

// phrase-level types ("pot filler", "shower head", …) also resolve to categories
var phraseGroups = map[string][]string{
	"pot filler": {"Pot Filler Faucets"}, "hand shower": {"Hand Showers"}, "shower head": {"Shower Heads"}, "rain head": {"Shower Heads"},
	"tub filler": {"Tub Fillers"}, "towel bar": {"Towel Bars & Rings"}, "towel ring": {"Towel Bars & Rings"}, "paper holder": {"Paper Holders"},
	"bar faucet": {"Bar & Prep Faucets"}, "prep faucet": {"Bar & Prep Faucets"}, "water closet": {"Toilets"},
	"medicine cabinet": {"Medicine Cabinets"}, "mirror cabinet": {"Medicine Cabinets"},
}

// CategoriesForType returns the categories a type concept label resolves to (nil = unknown).
func CategoriesForType(label string) []string {
	if g, ok := phraseGroups[label]; ok {
		return g
	}
	return typeGroups[singular(strings.ToLower(label))]
}

// categoriesForFilter resolves a tile/group name or a leaf category to the categories to
// retrieve; a leaf name maps to itself.
func categoriesForFilter(name string) []string {
	if g, ok := CategoryGroups[name]; ok {
		return g
	}
	return []string{name}
}

// categoryCore names the product a tile is *about*. Inside a tile, families whose title says
// that word lead the Featured order: on Filtration the filter systems (Body Glove, Housepure)
// come before the beverage faucets that are merely sold with a filter; on Toilets the toilets
// before the seats. Groups absent here get no boost (on Faucets every title says faucet).
var categoryCore = map[string]string{
	"Filtration":         `(filter|filtration|purif\w*) (system|unit|cartridge)s?|housepure|body ?glove|purifier`,
	"Water Filtration":   `(filter|filtration|purif\w*) (system|unit|cartridge)s?|housepure|body ?glove|purifier`,
	"Toilets":            `toilet|water closet|\bwc\b|bidet|urinal`,
	"Showers":            `shower (system|head|column|set)|showerhead|rain ?head|hand ?shower|body ?spray`,
	"Vanities":           `vanit|washstand|console`,
	"Medicine Cabinets":  `cabinet`,
	"Mirrors":            `mirror`,
	"Sinks":              `sink|basin|lavatory|vessel`,
	"Bathtubs":           `tub|bath`,
	"Pot Fillers":        `pot ?filler`,
	"Bar & Prep Faucets": `bar|prep`,
	"Cabinet Hardware":   `pull|knob|cup|appliance pull|hinge`,
	"Door Hardware":      `lever|knob|handleset|entry|deadbolt|passage|privacy`,
}

// CategoryCore returns the regex of what a tile or leaf category is about ("" = no boost).
func CategoryCore(name string) string { return categoryCore[name] }
