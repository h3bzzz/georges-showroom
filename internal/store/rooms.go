package store

import "strings"

// Rooms are the top-level navigation. Order matters for display.
var Rooms = []struct{ Slug, Name string }{
	{"bathroom", "Bathroom"}, {"kitchen", "Kitchen"}, {"hardware", "Hardware"}, {"appliances", "Appliances"},
}

var roomKeywords = map[string][]string{
	"appliances": {"range", "cooktop", "oven", "microwave", "dishwasher", "refrigerator", "freezer", "ice machine", "ice maker", "warming drawer", "trash compactor", "coffee", "hood", "ventilation", "laundry", "washer", "dryer", "wine", "appliance", "grill"},
	"hardware":   {"door hardware", "knob", "lever", "pull", "hinge", "door stop", "knocker", "doorbell", "door bell", "chime", "pocket door", "backplate", "cabinet", "hook", "house number", "mail slot", "handleset", "handle set", "deadbolt", "bolt", "window hardware", "window", "rosette", "escutcheon", "entry set", "dummy", "cylinder", "door part", "latch", "strike"},
	"kitchen":    {"kitchen", "chef", "culinary", "pot filler", "bar faucet", "prep faucet", "water dispenser", "hot water", "filtration", "filter", "soap dispenser", "garbage disposal", "disposal", "air gap", "colander", "cutting board"},
	"bathroom":   {"bath", "lavatory", "vessel", "pedestal", "console", "tub", "shower", "toilet", "bidet", "vanity", "vanities", "medicine cabinet", "mirror", "towel", "robe hook", "tissue", "steam", "sauna", "drain", "body spray", "hand shower", "urinal", "washbasin", "grab bar", "volume control", "faucet", "valve", "rough-in", "rough in", "trim", "spout", "diverter", "aerator"},
}

// brandRooms is the fallback when a family's category says nothing useful ("None").
var brandRooms = map[string]string{
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

// RoomFor assigns a family to a room from its category path, product type and title, falling
// back to the brand's usual room. Bathroom is checked last because "bath" words appear in
// many kitchen product names too (e.g. "bar faucet"), so the more specific rooms win.
func RoomFor(categoryPath []string, productType, title, brand string) string {
	text := strings.ToLower(strings.Join(categoryPath, " ") + " " + productType + " " + title)
	brandRoom := brandRooms[strings.ToLower(strings.TrimSpace(brand))]
	for _, room := range []string{"appliances", "hardware", "kitchen", "bathroom"} {
		for _, kw := range roomKeywords[room] {
			if !strings.Contains(text, kw) {
				continue
			}
			// a plumbing brand does not sell appliances or door hardware: words like "range"
			// or "hook" inside a faucet description must not flip the room
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
