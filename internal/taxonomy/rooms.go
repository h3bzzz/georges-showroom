// Package taxonomy is the merchandising vocabulary: rooms, categories, groups, which picture
// subjects may stand for which categories, and the finish dictionary. It is plain Go with
// tests so a rule change is a code review, not a migration.
package taxonomy

import "strings"

// Room is a top-level navigation entry.
type Room struct{ Slug, Name string }

// Rooms in display order.
var Rooms = []Room{{"bathroom", "Bathroom"}, {"kitchen", "Kitchen"}, {"hardware", "Hardware"}, {"appliances", "Appliances"}}

// RoomName returns the display name of a room slug ("" for unknown).
func RoomName(slug string) string {
	for _, r := range Rooms {
		if r.Slug == slug {
			return r.Name
		}
	}
	return ""
}

var roomKeywords = map[string][]string{
	"appliances": {"range", "cooktop", "oven", "microwave", "dishwasher", "refrigerator", "freezer", "ice machine", "ice maker", "warming drawer", "trash compactor", "coffee", "hood", "ventilation", "laundry", "washer", "dryer", "wine", "appliance", "grill"},
	"hardware":   {"door hardware", "knob", "lever", "pull", "hinge", "door stop", "knocker", "doorbell", "door bell", "chime", "pocket door", "backplate", "cabinet", "hook", "house number", "mail slot", "handleset", "handle set", "deadbolt", "bolt", "window hardware", "window", "rosette", "escutcheon", "entry set", "dummy", "cylinder", "door part", "latch", "strike"},
	"kitchen":    {"kitchen", "chef", "culinary", "pot filler", "bar faucet", "prep faucet", "water dispenser", "hot water", "filtration", "filter", "soap dispenser", "garbage disposal", "disposal", "air gap", "colander", "cutting board"},
	"bathroom":   {"bath", "lavatory", "vessel", "pedestal", "console", "tub", "shower", "toilet", "bidet", "vanity", "vanities", "medicine cabinet", "mirror", "towel", "robe hook", "tissue", "steam", "sauna", "drain", "body spray", "hand shower", "urinal", "washbasin", "grab bar", "volume control", "faucet", "valve", "rough-in", "rough in", "trim", "spout", "diverter", "aerator"},
}

// BrandRooms is the room a brand usually sells into: the fallback when a title says nothing.
var BrandRooms = map[string]string{
	"baldwin": "hardware", "emtek": "hardware", "rocky mountain hardware": "hardware", "ashley norton": "hardware",
	"top knobs": "hardware", "schaub and company": "hardware", "classic brass": "hardware", "designer doorware": "hardware",
	"du verre": "hardware", "grandeur hardware": "hardware", "nostalgic warehouse": "hardware", "atlas": "hardware",
	"buster + punch": "hardware", "manzoni": "hardware", "water street brass": "hardware", "trim by design": "hardware",
	"bosch": "appliances", "cove": "appliances", "sub-zero": "appliances", "wolf": "appliances", "thermador": "appliances",
	"miele": "appliances", "gaggenau": "appliances", "fisher & paykel": "appliances", "jenn-air": "appliances",
	"kitchen aid": "appliances", "lg appliances": "appliances", "sks": "appliances", "panasonic": "appliances",
	"blanco": "kitchen", "franke": "kitchen", "julien": "kitchen", "prochef by julien": "kitchen", "hamat": "kitchen",
	"waterstone": "kitchen", "kwc": "kitchen", "ukinox": "kitchen", "nantucket sinks": "kitchen", "foster": "kitchen",
	"aboutwater": "bathroom", "california faucets": "bathroom", "gessi": "bathroom", "newport brass": "bathroom", "kohler": "bathroom",
	"dornbracht": "bathroom", "brizo": "bathroom", "graff": "bathroom", "watermark": "bathroom", "phylrich": "bathroom", "rohl": "bathroom",
	"duravit": "bathroom", "dxv": "bathroom", "toto": "bathroom", "kallista": "bathroom", "lacava": "bathroom", "laufen": "bathroom",
	"native trails": "bathroom", "victoria + albert": "bathroom", "americh": "bathroom", "mti baths": "bathroom", "thg": "bathroom",
	"jaclo": "bathroom", "sigma": "bathroom", "crosswater london": "bathroom", "fantini": "bathroom", "franz viegener": "bathroom",
	"axent": "bathroom", "icera": "bathroom", "mr. steam": "bathroom", "stone forest": "bathroom", "strasser woodenworks": "bathroom",
	"james martin vanities": "bathroom", "electric mirror": "bathroom", "robern": "bathroom", "hydro systems": "bathroom",
	"glasscrafters": "bathroom",
}

// RoomFor assigns a family to a room from its category, product type and title, falling back
// to the brand's usual room. Bathroom is checked last because "bath" words appear in many
// kitchen names too ("bar faucet"), so the more specific rooms win.
func RoomFor(category, productType, title, brand string) string {
	text := strings.ToLower(category + " " + productType + " " + title)
	brandRoom := BrandRooms[strings.ToLower(strings.TrimSpace(brand))]
	for _, room := range []string{"appliances", "hardware", "kitchen", "bathroom"} {
		for _, kw := range roomKeywords[room] {
			if !strings.Contains(text, kw) {
				continue
			}
			// a plumbing brand does not sell appliances or door hardware: "range" or "hook"
			// inside a faucet description must not flip the room
			if room == "appliances" && brandRoom != "appliances" && !strings.Contains(text, "appliance") {
				break
			}
			if room == "hardware" && brandRoom != "hardware" && brandRoom != "" && !strongHardware(text) {
				break
			}
			return room
		}
	}
	return brandRoom
}

// a bare "hinge" is not strong: a bathroom brand's "Left Hinge" names a cabinet door side (Robern)
func strongHardware(text string) bool {
	for _, w := range []string{"door hardware", "cabinet knob", "cabinet pull", "handleset", "deadbolt", "door lever", "door knob", "door hinge", "cabinet hinge", "butt hinge", "entry set"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// Tile is a merchandised category shown on a room page.
type Tile struct{ Name, Keyword string }

// RoomTiles lists the tiles per room in display order.
var RoomTiles = map[string][]Tile{
	"bathroom":   {{"Faucets", "faucet"}, {"Sinks", "sink"}, {"Bathtubs", "tub"}, {"Showers", "shower"}, {"Toilets", "toilet"}, {"Vanities", "vanit"}, {"Mirrors", "mirror"}, {"Accessories", "accessor"}},
	"kitchen":    {{"Kitchen Faucets", "kitchen faucet"}, {"Kitchen Sinks", "sink"}, {"Pot Fillers", "pot filler"}, {"Bar & Prep Faucets", "bar"}, {"Filtration", "filter"}, {"Accessories", "accessor"}},
	"hardware":   {{"Cabinet Knobs", "knob"}, {"Cabinet Pulls", "pull"}, {"Door Levers", "lever"}, {"Door Knobs", "door knob"}, {"Entry Sets", "handleset"}, {"Hinges", "hinge"}, {"Bath Hardware", "bath"}, {"Locks & Deadbolts", "bolt"}},
	"appliances": {{"Ranges", "range"}, {"Cooktops", "cooktop"}, {"Ovens", "oven"}, {"Refrigeration", "refrigerat"}, {"Dishwashers", "dishwasher"}, {"Ventilation", "hood"}, {"Coffee", "coffee"}, {"Laundry", "laundry"}},
}
