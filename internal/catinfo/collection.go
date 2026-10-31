package catinfo

import (
	"regexp"
	"strings"
)

// typeWords never belong to a collection name: a heading "R3 Series Medicine Cabinets" names
// the R3 Series, "Roman Tub Faucets" names nothing.
var typeWords = map[string]bool{
	"faucet": true, "faucets": true, "sink": true, "sinks": true, "bidet": true, "bidets": true, "toilet": true, "toilets": true, "urinal": true, "urinals": true,
	"cabinet": true, "cabinets": true, "medicine": true, "vessel": true, "lavatory": true, "lavatories": true, "pedestal": true, "pedestals": true,
	"bath": true, "baths": true, "bathroom": true, "kitchen": true, "shower": true, "showers": true, "tub": true, "tubs": true, "bathtub": true, "bathtubs": true,
	"filler": true, "fillers": true, "mixer": true, "mixers": true, "washbasin": true, "washbasins": true, "basin": true, "basins": true, "hardware": true,
	"door": true, "doors": true, "accessories": true, "accessory": true, "pull": true, "pulls": true, "knob": true, "knobs": true, "lever": true, "levers": true,
	"handleset": true, "handlesets": true, "deadbolt": true, "deadbolts": true, "mortise": true, "set": true, "sets": true, "trim": true, "trims": true,
	"spout": true, "spouts": true, "price": true, "prices": true, "pricing": true, "list": true, "guide": true, "pricebook": true, "book": true, "catalog": true,
	"collection": true, "collections": true, "series": true, "line": true, "the": true, "and": true, "&": true, "by": true, "new": true, "with": true, "for": true,
	"wall": true, "mount": true, "mounted": true, "deck": true, "floor": true, "single": true, "double": true, "hole": true, "handle": true, "handles": true,
	"widespread": true, "centerset": true, "roman": true, "freestanding": true, "whirlpool": true, "soaking": true, "air": true, "thermal": true,
	"fireclay": true, "silgranit": true, "stainless": true, "steel": true, "solid": true, "brass": true, "bronze": true, "glossary": true, "overview": true,
	"index": true, "contents": true, "options": true, "upgrade": true, "upgrades": true, "models": true, "model": true, "description": true, "dimension": true,
	"dimensions": true, "finish": true, "finishes": true, "item": true, "items": true, "page": true, "pages": true, "section": true, "specifications": true,
	"specification": true, "features": true, "matching": true, "program": true, "quick": true, "ship": true, "reserve": true, "estate": true, "retail": true,
	"laundry": true, "bar": true, "prep": true, "pot": true, "head": true, "heads": true, "hand": true, "arm": true, "arms": true, "drain": true, "drains": true,
	"rough": true, "roughs": true, "valve": true, "valves": true, "thermostatic": true, "pressure": true, "balance": true, "custom": true, "digital": true,
	"steam": true, "system": true, "systems": true, "extension": true, "kit": true, "kits": true, "grab": true, "bars": true, "fittings": true, "filtration": true,
	"water": true, "instant": true, "hot": true, "decorative": true, "essential": true, "parts": true, "part": true, "component": true, "components": true,
	"table": true, "warranty": true, "terms": true, "notes": true, "note": true, "only": true, "all": true, "available": true, "continued": true,
	"commercial": true, "residential": true, "healthcare": true, "laboratory": true, "food": true, "service": true, "services": true, "products": true, "product": true,
	"mini": true, "semi": true, "pro": true, "soap": true, "dispenser": true, "dispensers": true, "disposer": true, "waste": true, "organizer": true, "caddy": true,
	"storage": true, "grid": true, "grids": true, "bowl": true, "apron": true, "undermount": true, "farmhouse": true, "classic": true, "traditional": true,
	"contemporary": true, "modern": true, "transitional": true, "us": true, "usa": true, "edition": true, "limited": true,
	"horizontal": true, "vertical": true, "spray": true, "flush": true, "rimless": true, "elongated": true, "round": true, "compact": true,
	"one-piece": true, "two-piece": true, "dual": true, "floorstanding": true, "floor-standing": true, "wall-mounted": true, "deck-mounted": true, "wall-mount": true, "deck-mount": true, "one": true, "two": true, "piece": true, "design": true, "designs": true, "questions": true,
	"ordering": true, "information": true, "sales": true, "policy": true, "policies": true, "shipping": true, "freight": true, "returns": true, "tank": true, "number": true, "no": true, "code": true, "qty": true, "stock": true,
	"type": true, "color": true, "colour": true, "sku": true, "ref": true, "glass": true, "mirror": true, "mirrors": true, "lighting": true,
	"vanity": true, "vanities": true, "console": true, "consoles": true, "washstand": true, "wood": true, "metal": true, "lavs": true, "lav": true,
	"slide": true, "body": true, "tap": true, "taps": true, "pillar": true, "edge": true, "edges": true, "appliance": true, "cup": true,
	"gooseneck": true, "hinges": true, "hinge": true, "soakers": true, "soaker": true, "supplies": true, "supply": true, "racks": true, "rack": true, "cylinder": true, "cylinders": true, "cartridge": true, "cartridges": true, "outlet": true, "outlets": true, "blaticiceid": true, "rods": true, "rod": true, "bolts": true, "bolt": true, "surface": true, "combinations": true,
	"combination": true, "selling": true, "best": true, "entrance": true, "tubular": true, "modifications": true, "modification": true, "specialty": true,
	"handshowers": true, "handshower": true, "discontinued": true, "coming": true, "soon": true, "numerical": true, "listing": true, "builder": true,
	"luxury": true, "platinum": true, "rosettes": true, "rosette": true, "raincan": true, "strainer": true, "basket": true, "pg": true, "plates": true,
	"plate": true, "escutcheons": true, "escutcheon": true, "roses": true, "rose": true, "backplate": true, "castlron": true, "iron": true, "cast": true,
	"showerhead": true, "showerheads": true, "sprayers": true, "sprayer": true,
	"cabinetry": true, "furniture": true, "tops": true, "top": true, "seats": true, "seat": true, "sprays": true, "jets": true, "diverter": true, "diverters": true, "controls": true, "control": true,
}

var (
	reCollNoise = regexp.MustCompile(`(?i)\b(19|20)\d\d\b|[™®"'’,:;.!?]+|\(.*?\)|\bp\.\s*\d+`)
	reNumber    = regexp.MustCompile(`^\d{2,}$`)
	reDesignBy  = regexp.MustCompile(`(?i)\bdesign(?:ed)? by\b`)
	reVowels    = regexp.MustCompile(`[aeiouyAEIOUY]`)
	reConsRun   = regexp.MustCompile(`(?i)[bcdfghjklmnpqrstvwxz]{4}`)
	reConsOK    = regexp.MustCompile(`(?i)sch|tch|str|nst|ngt|rst|nth|rth|ndr|mpt|lts|rks|rst|nds|rds|lds`)
	reCollSplit = regexp.MustCompile(`\s*[·|]\s*|\s+[-/]\s+`)
)

// Collection cleans a heading or contents entry into a collection name, or "" when the text
// names a product type, the maker or a reference page rather than a line of products.
// "Descanso · K30 Series" → "Descanso"; "R3 Series Medicine Cabinets" → "R3 Series";
// "DuraSquare Vessel Sink Design by Duravit" → "DuraSquare"; "Roman Tub Faucets" → "".
func Collection(s, brand string) string {
	s = Norm(s)
	if loc := reDesignBy.FindStringIndex(s); loc != nil {
		s = s[:loc[0]] // "ME by Starck … Design by Philippe Starck": the designer is not the line
	}
	s = reCollNoise.ReplaceAllString(s, " ")
	// "Descanso · K30 Series": the name is the first part; "Jason Wu for Brizo" stays whole
	if parts := reCollSplit.Split(s, -1); len(parts) > 1 {
		s = parts[0]
	}
	var keep []string
	for _, w := range strings.Fields(s) {
		lw := strings.ToLower(w)
		if reNumber.MatchString(w) {
			continue // "Vintera 33": a size, not part of the name
		}
		if lw == "by" && len(keep) > 0 {
			keep = append(keep, w) // "ME by Starck" (a trailing "by" is trimmed below)
			continue
		}
		if typeWords[lw] {
			if (lw == "series" || lw == "collection") && len(keep) > 0 && isCode(keep[len(keep)-1]) {
				keep = append(keep, titleCase(w)) // "R3 Series", "C2 Series": the code needs the word
			}
			continue
		}
		if !goodCollWord(w) {
			return ""
		}
		keep = append(keep, w)
	}
	for len(keep) > 0 && strings.EqualFold(keep[len(keep)-1], "by") {
		keep = keep[:len(keep)-1]
	}
	if len(keep) == 0 || len(keep) > 4 {
		return ""
	}
	if len(keep) == 1 && genericNames[strings.ToLower(keep[0])] {
		return "" // "Square" from a "Square Spout" heading is a shape, not a line
	}
	caps, title := 0, 0
	for i, w := range keep {
		if i > 0 {
			a, b := strings.ToLower(keep[i-1]), strings.ToLower(w)
			if len(a) >= 4 && len(b) >= 4 && a[:4] == b[:4] {
				return "" // "Modele Modelo Modello": a heading repeated in three languages
			}
		}
		if isCode(w) || strings.ToLower(w) == strings.ToUpper(w) {
			continue // codes and bare numbers have no case
		}
		if strings.ToUpper(w) == w {
			caps++
		} else {
			title++
		}
	}
	if caps > 0 && title > 0 {
		return "" // "Handiefndex CTRREON Canridge": mixed case styles are OCR debris, not a name
	}
	out := strings.Join(keep, " ")
	lb := strings.ToLower(brand)
	if lo := strings.ToLower(out); lb != "" && (strings.Contains(lb, lo) || strings.Contains(lo, lb)) {
		return "" // the maker's own name
	}
	if strings.ToUpper(out) == out {
		out = titleCase(out)
		for i, w := range strings.Fields(out) {
			if isCode(keep[i]) {
				out = strings.Replace(out, w, strings.ToUpper(w), 1)
			}
		}
	}
	return out
}

// reWordShape: the case patterns a printed name word can have (Allaria, DURAVIT, DuraSquare,
// D-Neo, McKenzie). "ANco" and "fObefn" are OCR debris.
var reWordShape = regexp.MustCompile(`^(?:[A-Z]+|(?:[A-Z][a-z]+)+|[A-Z]?[a-z]+|[A-Z]\d*)(?:-(?:[A-Z]+|(?:[A-Z][a-z]+)+|[A-Z]?[a-z]+|\d+))?$`)

// genericNames are shape and quality words a heading may leave behind; alone they never
// name a collection.
var genericNames = map[string]bool{
	"square": true, "round": true, "arc": true, "linear": true, "standard": true, "deluxe": true, "basic": true, "premium": true,
	"universal": true, "original": true, "signature": true, "select": true, "oval": true, "rectangular": true, "curved": true,
	"straight": true, "angled": true, "high": true, "low": true, "large": true, "small": true, "medium": true, "long": true, "short": true,
	"left": true, "right": true, "front": true, "rear": true, "top": true, "bottom": true, "side": true, "corner": true, "center": true,
	"black": true, "white": true, "chrome": true, "nickel": true, "gold": true, "silver": true, "matte": true, "polished": true, "brushed": true,
	"satin": true, "smooth": true, "knurled": true, "fluted": true, "textured": true, "plain": true, "simple": true, "pure": true, "total": true,
	"general": true, "special": true, "other": true, "misc": true, "miscellaneous": true, "various": true, "additional": true, "extra": true,
	"complete": true, "full": true, "half": true, "mini": true, "maxi": true, "super": true, "ultra": true, "plus": true, "pro": true, "xl": true,
	"decorative": true, "functional": true, "optional": true, "replacement": true, "spare": true, "ordering": true, "information": true,
	"handle": true, "handles": true, "cross": true, "lever": true, "knob": true, "wristblade": true, "spout": true, "trim": true, "rough": true,
	"inlet": true, "outlet": true, "thermostatic": true, "overview": true, "catalog": true, "introduction": true, "about": true, "contact": true,
	"featured": true, "feature": true, "features": true, "benefits": true, "color": true, "colors": true, "finish": true, "finishes": true,
	"model": true, "models": true, "sizes": true, "size": true, "elongated": true, "compact": true, "wall": true, "floor": true, "deck": true,
	"shower": true, "tub": true, "sink": true, "faucet": true, "toilet": true, "bidet": true, "vanity": true, "mirror": true, "cabinet": true,
	"accessories": true, "set": true, "sets": true, "kit": true, "kits": true, "system": true, "systems": true, "parts": true, "price": true,
}

// isCode: a short model-line code such as R3, C2, K30, PL, 85/86.
func isCode(w string) bool {
	if len(w) > 5 {
		return false
	}
	letters, digits := 0, 0
	for _, r := range w {
		switch {
		case r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		case r == '/' || r == '-':
		default:
			return false
		}
	}
	return letters > 0 && letters <= 2 && digits > 0 || letters == 1 && digits == 0 || letters == 2 && digits == 0 && w == strings.ToUpper(w) ||
		letters == 0 && digits > 0 && strings.Contains(w, "/")
}

func goodCollWord(w string) bool {
	if isCode(w) {
		return true
	}
	if len(w) == 1 && w[0] >= '0' && w[0] <= '9' {
		return true // "Starck 3"
	}
	if len(w) < 2 || len(w) > 14 {
		return false
	}
	if !reWordShape.MatchString(strings.Trim(w, "'")) {
		return false // "ANco", "fObefn": OCR debris
	}
	if len(w) >= 4 && !reVowels.MatchString(w) {
		return false // no vowel at all: OCR debris
	}
	if run := reConsRun.FindString(w); run != "" && !reConsOK.MatchString(run) {
		return false // "Wramszs", "Pllsse", "Iwwrbses": not pronounceable, not a name
	}
	letters := 0
	for _, r := range w {
		switch {
		case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			letters++
		case r == '-' || r == '\'':
		case r >= '0' && r <= '9':
			return false // a page number or a year glued to a word
		default:
			return false
		}
	}
	if letters < 2 || letters < len(w)-1 {
		return false
	}
	if r := w[0]; r >= 'a' && r <= 'z' {
		return false // mid-sentence text, not a name
	}
	// three identical letters in a row never happens in a name: OCR debris
	lw := strings.ToLower(w)
	for i := 2; i < len(lw); i++ {
		if lw[i] == lw[i-1] && lw[i] == lw[i-2] {
			return false
		}
	}
	return true
}

// reSeriesName finds "Odin Collection", "Allaria™ Bath Collection", "M Series" in prose.
var reSeriesName = regexp.MustCompile(`\b([A-Z][A-Za-z'-]{2,}(?: [A-Z][A-Za-z'-]{2,})?|[A-Z]\d?) (?:Bath |Kitchen )?(?:Collection|Series)\b`)

// collectionInText returns the first collection named in prose, or "".
func collectionInText(s, brand string) string {
	for _, m := range reSeriesName.FindAllStringSubmatch(s, 4) {
		if c := Collection(m[0], brand); c != "" {
			return c
		}
	}
	return ""
}
