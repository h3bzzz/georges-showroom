package taxonomy

import "strings"

// typeGroups maps a product-type word in a query to the categories it means.
var typeGroups = map[string][]string{
	"faucet": Groups["Faucets"], "tap": Groups["Faucets"], "mixer": Groups["Faucets"],
	"sink": Groups["Sinks"], "basin": {"Bathroom Sinks"}, "lavatory": {"Bathroom Sinks", "Bathroom Sink Faucets"},
	"toilet": {"Toilets"}, "bidet": {"Bidets", "Bidet Faucets"}, "urinal": {"Urinals"},
	"tub": {"Bathtubs", "Tub Fillers"}, "bathtub": {"Bathtubs"},
	"shower": Groups["Showers"], "showerhead": {"Shower Heads"}, "handshower": {"Hand Showers"},
	"filler": {"Tub Fillers", "Pot Filler Faucets"}, "potfiller": {"Pot Filler Faucets"},
	"vanity": {"Vanities"}, "cabinet": {"Vanities", "Medicine Cabinets"}, "mirror": {"Mirrors"},
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

// phraseGroups are two-word types.
var phraseGroups = map[string][]string{
	"pot filler": {"Pot Filler Faucets"}, "hand shower": {"Hand Showers"}, "shower head": {"Shower Heads"}, "rain head": {"Shower Heads"},
	"tub filler": {"Tub Fillers"}, "towel bar": {"Towel Bars & Rings"}, "towel ring": {"Towel Bars & Rings"}, "paper holder": {"Paper Holders"},
	"bar faucet": {"Bar & Prep Faucets"}, "prep faucet": {"Bar & Prep Faucets"}, "water closet": {"Toilets"},
	"medicine cabinet": {"Medicine Cabinets"}, "mirror cabinet": {"Medicine Cabinets"}, "kitchen faucet": {"Kitchen Faucets"},
	"kitchen sink": {"Kitchen Sinks"}, "bathroom faucet": {"Bathroom Sink Faucets"}, "bathroom sink": {"Bathroom Sinks"},
	"shower system": {"Shower Systems"}, "body spray": {"Body Sprays"}, "grab bar": {"Grab Bars"}, "robe hook": {"Towel Bars & Rings"},
	"cabinet pull": {"Cabinet Pulls"}, "cabinet knob": {"Knobs"}, "door lever": {"Levers"}, "door knob": {"Knobs"}, "entry set": {"Handle Sets"},
}

// CategoriesForType resolves a type word or phrase to categories (nil = not a type).
func CategoriesForType(label string) []string {
	l := strings.ToLower(strings.TrimSpace(label))
	if g, ok := phraseGroups[l]; ok {
		return g
	}
	return typeGroups[Singular(l)]
}

// IsTypeWord reports whether a single word names a product type.
func IsTypeWord(w string) bool { _, ok := typeGroups[Singular(strings.ToLower(w))]; return ok }

// Singular strips a plural ending (faucets → faucet, vanities → vanity).
func Singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "sses"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}
