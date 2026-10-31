package store

// RoomCategory is a merchandised category shown as a tile on a room page. Keyword is what
// product categories/types/titles are matched against (case-insensitive substring); Photo is
// the word used to find a lifestyle photograph in the catalogs.
type RoomCategory struct {
	Name, Keyword, Photo string
}

var RoomCategories = map[string][]RoomCategory{
	"bathroom": {
		{"Faucets", "faucet", "faucet"}, {"Sinks", "sink", "basin"}, {"Bathtubs", "tub", "tub"},
		{"Showers", "shower", "shower"}, {"Toilets", "toilet", "toilet"}, {"Vanities", "vanit", "vanit"},
		{"Mirrors", "mirror", "mirror"}, {"Accessories", "accessor", "towel"},
	},
	"kitchen": {
		{"Kitchen Faucets", "kitchen faucet", "kitchen"}, {"Kitchen Sinks", "sink", "sink"}, {"Pot Fillers", "pot filler", "pot filler"},
		{"Bar & Prep Faucets", "bar", "bar"}, {"Filtration", "filter", "filter"}, {"Accessories", "accessor", "colander"},
	},
	"hardware": {
		{"Cabinet Knobs", "knob", "knob"}, {"Cabinet Pulls", "pull", "pull"}, {"Door Levers", "lever", "lever"},
		{"Door Knobs", "door knob", "knob"}, {"Entry Sets", "handleset", "entry"}, {"Hinges", "hinge", "hinge"},
		{"Bath Hardware", "bath", "towel"}, {"Locks & Deadbolts", "bolt", "deadbolt"},
	},
	"appliances": {
		{"Ranges", "range", "range"}, {"Cooktops", "cooktop", "cooktop"}, {"Ovens", "oven", "oven"},
		{"Refrigeration", "refrigerat", "refrigerat"}, {"Dishwashers", "dishwasher", "dishwasher"}, {"Ventilation", "hood", "hood"},
		{"Coffee", "coffee", "coffee"}, {"Laundry", "laundry", "laundry"},
	},
}
