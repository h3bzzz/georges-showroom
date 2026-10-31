package catinfo

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Input is everything the store knows about one family on one catalog page.
type Input struct {
	Brand     string   // brand code (BRZ)
	BrandName string   // Brizo
	Title     string   // family title
	Room      string   // bathroom | kitchen | hardware | appliances | ""
	Category  string   // merchandised category, "" when none
	SKUs      []string // the family's SKUs
	Own       []string // the family's own table rows (the lines that hold its SKUs)
	Block     []string // lines of the family's block on the page, reading order
	Heading   string   // page heading ("DESCANSO 30/31 SERIES")
	TOC       []string // contents entries covering the page, most trusted first
	PageSKUs  []string // every SKU token on the page
	Spread    int      // pages across all books that carry the family; parts listed everywhere get own-row facts only
}

// Fact is one typed attribute. Conf is the parser's confidence in [0,1].
type Fact struct {
	Key, Value, Unit string
	Conf             float64
}

// Result is what one page block yields for a family.
type Result struct {
	Facts       []Fact
	Collection  string
	CollConf    float64
	Description string
	Bullets     []string
	Finishes    []string // finish names printed for the family (any source)
	RowFinish   string   // the one finish the family's own rows name, "" when none or several
}

type ex struct {
	in    Input
	facts map[string]Fact
	order []string
	own   string // flattened own lines + title
	text  string // flattened block
	skus  map[string]bool
}

func (e *ex) add(key, value, unit string, conf float64) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if f, ok := e.facts[key]; ok {
		if conf <= f.Conf {
			return
		}
	} else {
		e.order = append(e.order, key)
	}
	e.facts[key] = Fact{key, value, unit, conf}
}

func (e *ex) has(key string) bool { _, ok := e.facts[key]; return ok }

// Extract runs every rule over the input and returns the facts found.
func Extract(in Input) Result {
	e := &ex{in: in, facts: map[string]Fact{}, skus: map[string]bool{}}
	for _, s := range in.PageSKUs {
		e.skus[strings.ToUpper(s)] = true
	}
	for _, s := range in.SKUs {
		e.skus[strings.ToUpper(s)] = true
	}
	ownLines := append([]string{in.Title}, in.Own...)
	e.own = Norm(Flat(ownLines))
	if in.Spread > 4 {
		in.Block = nil // a handle kit printed on forty pages must not inherit forty neighbours' specs
	}
	block := make([]string, len(in.Block))
	for i, l := range in.Block {
		block[i] = Norm(l)
	}
	e.text = Flat(block)

	// own rows and title first (high confidence), then the block
	for _, src := range []struct {
		t    string
		conf float64
	}{{e.own, 0.9}, {e.text, 0.7}} {
		e.dims(src.t, src.conf)
		e.flow(src.t, src.conf)
		e.mounting(src.t, src.conf)
		e.holesHandles(src.t, src.conf)
		e.material(src.t, src.conf)
		e.misc(src.t, src.conf)
	}
	e.hardware()
	e.blanco()
	e.availability()
	e.worksWith()

	var r Result
	for _, k := range e.order {
		r.Facts = append(r.Facts, e.facts[k])
	}
	r.Collection, r.CollConf = e.collection()
	r.Bullets = Bullets(append([]string(nil), in.Block...), e.skus, in.Title)
	r.Description = Description(in.Block, e.skus)
	r.Finishes = e.finishes()
	r.RowFinish = rowFinish(in.Own)
	return r
}

// ---------- dimensions ----------

var dimKeyPre = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`(?i)overall height\s*[=:]?\s*$`), "Overall Height"},
	{regexp.MustCompile(`(?i)\bmax(?:imum|\.)? height\s*[=:]?\s*$`), "Max Height"},
	{regexp.MustCompile(`(?i)\b(?:max(?:imum|\.)? )?arm reach\s*[=:]?\s*$`), "Arm Reach"},
	{regexp.MustCompile(`(?i)\bhose(?: length)?\s*[=:]?\s*$`), "Hose Length"},
	{regexp.MustCompile(`(?i)spout (?:outlet )?(?:to deck|height|ht\.?)\s*[=:]?\s*$`), "Spout Height"},
	{regexp.MustCompile(`(?i)spout (?:reach|projection|proj\.?|length)\s*[=:]?\s*$`), "Spout Reach"},
	{regexp.MustCompile(`(?i)\bbowl depth\s*[=:]?\s*$`), "Bowl Depth"},
	{regexp.MustCompile(`(?i)\bbasin depth\s*[=:]?\s*$`), "Basin Depth"},
	{regexp.MustCompile(`(?i)\b(?:height|ht\.?)\s*[=:]?\s*$`), "Height"},
	{regexp.MustCompile(`(?i)\bwidth\s*[=:]?\s*$`), "Width"},
	{regexp.MustCompile(`(?i)\bdepth\s*[=:]?\s*$`), "Depth"},
	{regexp.MustCompile(`(?i)\blength\s*[=:]?\s*$`), "Length"},
	{regexp.MustCompile(`(?i)\bprojection(?: of)?\s*[=:]?\s*$`), "Projection"},
	{regexp.MustCompile(`(?i)\bdia(?:meter|\.)?\s*[=:]?\s*$`), "Diameter"},
	{regexp.MustCompile(`(?i)\b(?:ctc|c-c|c/c|center[- ]to[- ]center|centers)\s*[=:]?\s*$`), "Centers"},
	{regexp.MustCompile(`(?i)\bbackset\s*[=:]?\s*$`), "Backset"},
	{regexp.MustCompile(`(?i)\brough-?in\s*[=:]?\s*$`), "Rough-In"},
	{regexp.MustCompile(`(?i)\bmin(?:imum|\.)? cabinet(?: size| width)?\s*[=:]?\s*$`), "Minimum Cabinet"},
	{regexp.MustCompile(`(?i)\bdoor thickness\s*[=:]?\s*(?:up to\s*)?$`), "Door Thickness"},
	{regexp.MustCompile(`(?i)\bup to\s*$`), "Max Deck Thickness"}, // "Accommodates up to 2-1/2" deck thickness" (checked with post below)
	{regexp.MustCompile(`\(K\)\s*\d*\s*$`), "Knob Diameter"},
	{regexp.MustCompile(`\(R\)\s*\d*\s*$`), "Rose Diameter"},
	{regexp.MustCompile(`(?i)\bW\s*$`), "Width"},
	{regexp.MustCompile(`(?i)\bD\s*$`), "Depth"},
	{regexp.MustCompile(`(?i)\bH\s*$`), "Height"},
}

var dimKeyPost = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`^W\b`), "Width"},
	{regexp.MustCompile(`^H\b`), "Height"},
	{regexp.MustCompile(`^D\b`), "Depth"},
	{regexp.MustCompile(`^L\b`), "Length"},
	{regexp.MustCompile(`(?i)^\s?(?:spout )?reach\b`), "Spout Reach"},
	{regexp.MustCompile(`(?i)^\s?(?:spout )?projection\b`), "Spout Reach"},
	{regexp.MustCompile(`(?i)^\s?wall to (?:center|centre)\b`), "Spout Reach"},
	{regexp.MustCompile(`(?i)^\s?(?:gooseneck|swing|swivel|rigid|high[- ]arc|cast|straight)? ?spout\b`), "Spout Reach"},
	{regexp.MustCompile(`(?i)^\s?(?:fixed |adjustable )?centers\b`), "Centers"},
	{regexp.MustCompile(`(?i)^\s?(?:ctc|c-c|c/c|cc|c to c|center[- ]to[- ]center|on center)\b`), "Centers"},
	{regexp.MustCompile(`(?i)^\s?centerset\b`), "Centers"},
	{regexp.MustCompile(`(?i)^\s?(?:mini-?)?widespread\b`), "Centers"},
	{regexp.MustCompile(`(?i)^\s?backset\b`), "Backset"},
	{regexp.MustCompile(`(?i)^\s?rough-?in\b`), "Rough-In"},
	{regexp.MustCompile(`(?i)^\s?(?:FIP|MIP|NPT|NPSM|IPS|NPTF|BSP|FNPT|MNPT)\b`), "Inlet"},
	{regexp.MustCompile(`(?i)^\s?deck(?: thickness)?\b`), "Max Deck Thickness"},
	{regexp.MustCompile(`(?i)^\s?(?:-\s?\d[\d\-/]*"?)?[^.;"]{0,32}?\bhose\b`), "Hose Length"},
	{regexp.MustCompile(`(?i)^\s?(?:overall )?height\b`), "Height"},
	{regexp.MustCompile(`(?i)^\s?(?:overall )?width\b`), "Width"},
	{regexp.MustCompile(`(?i)^\s?(?:overall )?depth\b`), "Depth"},
	{regexp.MustCompile(`(?i)^\s?(?:overall )?length\b`), "Length"},
	{regexp.MustCompile(`(?i)^\s?dia(?:meter|\.)?\b`), "Diameter"},
	{regexp.MustCompile(`(?i)^\s?(?:door )?thick(?:ness)?\b`), "Door Thickness"},
	{regexp.MustCompile(`(?i)^\s?(?:ceiling|wall)[- ]mount(?:ed)? shower arm\b|^\s?shower arm\b`), "Arm Length"},
}

var pairKeyPre = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`(?i)basin interior\s*[=:]?\s*$`), "Basin Interior"},
	{regexp.MustCompile(`(?i)bowl (?:size|dimensions?)\s*[=:]?\s*$`), "Bowl Size"},
	{regexp.MustCompile(`(?i)cut-?out(?: size| dimensions?)?\s*[=:]?\s*$`), "Cutout"},
	{regexp.MustCompile(`(?i)interior(?: dimensions?)?\s*[=:]?\s*$`), "Interior Dimensions"},
	{regexp.MustCompile(`(?i)(?:overall|exterior)(?: dimensions?| size)?\s*[=:]?\s*$`), "Overall Dimensions"},
	{regexp.MustCompile(`(?i)(?:dimensions?|size|measures)\s*[=:]?\s*$`), "Dimensions"},
}

// dimLimits are the plausible ranges in inches per key; anything outside is an OCR misread
// ("W 145"" for a pedestal sink that is 14-5/8" wide).
var dimLimits = map[string][2]float64{
	"Overall Height": {1, 60}, "Spout Height": {0.5, 40}, "Spout Reach": {1, 30}, "Height": {0.25, 120}, "Width": {0.25, 120},
	"Depth": {0.25, 120}, "Length": {0.25, 120}, "Projection": {0.1, 24}, "Diameter": {0.1, 48}, "Centers": {2, 16}, "Backset": {2, 3},
	"Rough-In": {8, 16}, "Bowl Depth": {2, 14}, "Basin Depth": {2, 14}, "Minimum Cabinet": {9, 60}, "Door Thickness": {1, 3.5},
	"Hose Length": {24, 120}, "Max Deck Thickness": {0.5, 4}, "Knob Diameter": {0.5, 4}, "Rose Diameter": {1, 4}, "Inlet": {0.2, 1.5},
	"Arm Length": {2, 24}, "Max Height": {1, 60}, "Arm Reach": {1, 30},
}

var reX = regexp.MustCompile(`^\s?x\s?$`)

func (e *ex) dims(t string, conf float64) {
	hits := findDims(t)
	used := make([]bool, len(hits))
	// labelled pairs/triples: "16"W x 20"H x 4"D", "23-5/8" x 18-1/2"", "72"L x 40"W x 27"D - center drain"
	for i := 0; i < len(hits); i++ {
		if used[i] {
			continue
		}
		run := []int{i}
		for j := i; j+1 < len(hits); j++ {
			between := t[hits[j].End:hits[j+1].Start]
			between = regexp.MustCompile(`^[WHDL]\b`).ReplaceAllString(between, "")
			if !reX.MatchString(between) {
				break
			}
			run = append(run, j+1)
		}
		if len(run) < 2 {
			continue
		}
		labelled := 0
		var parts []string
		for _, k := range run {
			used[k] = true
			post := t[hits[k].End:min(len(t), hits[k].End+3)]
			parts = append(parts, hits[k].String())
			for _, r := range dimKeyPost[:4] {
				if r.re.MatchString(post) {
					labelled++
					e.addDim(r.key, hits[k].Dim, conf)
					break
				}
			}
		}
		if labelled == 0 && len(run) <= 3 {
			pre := t[max(0, hits[run[0]].Start-28):hits[run[0]].Start]
			key := "Dimensions"
			for _, r := range pairKeyPre {
				if r.re.MatchString(pre) {
					key = r.key
					break
				}
			}
			ok := true
			for _, k := range run {
				if v := hits[k].Inches(); v < 0.5 || v > 120 {
					ok = false
				}
			}
			if ok {
				e.add(key, strings.Join(parts, " x "), "in", conf-0.1)
			}
		}
	}
	for i, h := range hits {
		if used[i] {
			continue
		}
		pre := t[max(0, h.Start-40):h.Start]
		post := t[h.End:min(len(t), h.End+40)]
		key := ""
		for _, r := range dimKeyPre {
			if r.re.MatchString(pre) {
				key = r.key
				break
			}
		}
		if key == "" || key == "Max Deck Thickness" {
			for _, r := range dimKeyPost {
				if r.re.MatchString(post) {
					key = r.key
					break
				}
			}
			if key == "Max Deck Thickness" && !strings.Contains(strings.ToLower(post), "deck") {
				key = ""
			}
		}
		if key == "" {
			continue
		}
		if key == "Inlet" && h.Inches() > 1.5 {
			continue
		}
		if key == "Centers" {
			if strings.Contains(strings.ToLower(post), "widespread") && !e.has("Faucet Holes") {
				e.add("Faucet Holes", "3", "", conf)
			}
			if strings.Contains(strings.ToLower(post), "centerset") && !e.has("Faucet Holes") {
				e.add("Faucet Holes", "3", "", conf)
			}
		}
		if key == "Backset" || key == "Door Thickness" || key == "Hose Length" {
			// ranges: "2.375" and 2.75"", "1-3/8" - 1-3/4"", "60"-82""
			if i+1 < len(hits) && regexp.MustCompile(`^\s?(?:and|or|to|-|/)\s?$`).MatchString(t[h.End:hits[i+1].Start]) {
				if lim, ok := dimLimits[key]; !ok || hits[i+1].Inches() >= lim[0] && hits[i+1].Inches() <= lim[1] {
					sep := " and "
					if key != "Backset" {
						sep = " - "
					}
					e.addDimText(key, h.String()+sep+hits[i+1].String(), h.Dim, conf)
					used[i+1] = true
					continue
				}
			}
		}
		if key == "Inlet" {
			unit := strings.ToUpper(strings.Fields(post)[0])
			e.addDimText(key, h.String()+" "+unit, h.Dim, conf)
			continue
		}
		e.addDim(key, h.Dim, conf)
	}
	// hardware latches: two backset values joined by "and" without the word nearby
	if e.in.Room == "hardware" && !e.has("Backset") {
		if m := regexp.MustCompile(`(?i)\blatch\b.*?(2(?:\.375|\.75|-3/8|-3/4| 3/8| 3/4)")\s?(?:and|or|/)\s?(2(?:\.375|\.75|-3/8|-3/4| 3/8| 3/4)")`).FindStringSubmatch(t); m != nil {
			e.add("Backset", m[1]+" and "+m[2], "in", conf-0.1)
		}
	}
}

func (e *ex) addDim(key string, d Dim, conf float64) {
	e.addDimText(key, d.String(), d, conf)
}

func (e *ex) addDimText(key, text string, d Dim, conf float64) {
	if lim, ok := dimLimits[key]; ok {
		if v := d.Inches(); v < lim[0] || v > lim[1] {
			return
		}
	}
	unit := "in"
	if d.Unit == "mm" {
		unit = "mm"
	}
	e.add(key, text, unit, conf)
}

// ---------- flow ----------

var (
	reGPM  = regexp.MustCompile(`(?i)(\d{1,2}(?:[.,]\d{1,2})?)\s?gpm(?:\s?(?:@|at)\s?(\d{2,3})\s?psi)?`)
	reLPM  = regexp.MustCompile(`(?i)(\d{1,2}(?:[.,]\d{1,2})?)\s?(?:l/min|lpm|l/m)\b`)
	reGPF  = regexp.MustCompile(`(?i)(\d(?:\.\d{1,2})?)(?:\s?/\s?(\d(?:\.\d{1,2})?))?\s?(?:gpf|gal(?:lons?)? per flush)`)
	reFull = regexp.MustCompile(`(?i)(\d\.\d)\s?gpm to full flow`)
)

func (e *ex) flow(t string, conf float64) {
	if m := reFull.FindStringSubmatch(t); m != nil {
		e.add("Flow Rate", m[1]+" gpm to full flow", "gpm", conf)
	}
	if m := reGPM.FindStringSubmatch(t); m != nil {
		v := strings.ReplaceAll(m[1], ",", ".")
		g, _ := strconv.ParseFloat(v, 64)
		if lm := reLPM.FindStringSubmatch(t); lm != nil {
			l, _ := strconv.ParseFloat(strings.ReplaceAll(lm[1], ",", "."), 64)
			if l > 0 && g > 0 && g*3.785 > l*1.3 && (g/10)*3.785 < l*1.3 && (g/10)*3.785 > l*0.7 {
				g = g / 10 // "20 gpm, 7.6 L/min": the OCR dropped the decimal point
				v = strconv.FormatFloat(g, 'f', 1, 64)
			}
		}
		if g >= 0.3 && g <= 30 {
			if !strings.Contains(v, ".") && g < 10 {
				v += ".0"
			}
			val := v + " gpm"
			if m[2] != "" {
				val += " @ " + m[2] + " psi"
			}
			e.add("Flow Rate", val, "gpm", conf)
		}
	}
	if m := reGPF.FindStringSubmatch(t); m != nil {
		g, _ := strconv.ParseFloat(m[1], 64)
		if g >= 0.5 && g <= 3.5 {
			val := m[1] + " gpf"
			if m[2] != "" {
				val = m[1] + " / " + m[2] + " gpf (dual flush)"
			}
			e.add("Flush Volume", val, "gpf", conf)
		}
	}
}

// ---------- mounting ----------

var mountRules = []struct {
	re  *regexp.Regexp
	val string
}{
	{regexp.MustCompile(`(?i)\bdeck[- ]mount`), "Deck Mount"},
	{regexp.MustCompile(`(?i)\bwall[- ]mount|\bwall[- ]hung\b|\bwall-?mounted\b`), "Wall Mount"},
	{regexp.MustCompile(`(?i)\bceiling[- ]mount`), "Ceiling Mount"},
	{regexp.MustCompile(`(?i)\bfloor[- ]?standing\b|\bfree[- ]?standing\b`), "Freestanding"},
	{regexp.MustCompile(`(?i)\bfloor[- ]mount`), "Floor Mount"},
	{regexp.MustCompile(`(?i)\bunder-?mount`), "Undermount"},
	{regexp.MustCompile(`(?i)\bdrop-?in\b|\btop-?mount|\bself-?rimming\b`), "Drop-In"},
	{regexp.MustCompile(`(?i)\bsemi-?recessed\b`), "Semi-Recessed"},
	{regexp.MustCompile(`(?i)\brecessed\b`), "Recessed"},
	{regexp.MustCompile(`(?i)\bsurface[- ]mount`), "Surface Mount"},
	{regexp.MustCompile(`(?i)\bin-?wall\b`), "In-Wall"},
	{regexp.MustCompile(`(?i)\bvessel\b`), "Vessel"},
	{regexp.MustCompile(`(?i)\bpedestal\b`), "Pedestal"},
	{regexp.MustCompile(`(?i)\balcove\b`), "Alcove"},
	{regexp.MustCompile(`(?i)\bcorner\b`), "Corner"},
}

var reAlsoAvail = regexp.MustCompile(`(?i)(also available|available as|\bor\b|optional|option)\s*(?:as\s*)?(?:a\s*)?$`)

func (e *ex) mounting(t string, conf float64) {
	if e.has("Mounting") || e.in.Room == "hardware" || e.in.Room == "appliances" {
		return
	}
	for _, r := range mountRules {
		for _, loc := range r.re.FindAllStringIndex(t, 3) {
			pre := t[max(0, loc[0]-24):loc[0]]
			if reAlsoAvail.MatchString(pre) {
				continue
			}
			if r.val == "Vessel" || r.val == "Pedestal" || r.val == "Corner" {
				// "vessel mixer" is a faucet type, not a mounting: only sinks/tubs carry these
				if !strings.Contains(e.in.Category, "Sink") && !strings.Contains(e.in.Category, "Bathtub") {
					continue
				}
			}
			e.add("Mounting", r.val, "", conf)
			return
		}
	}
}

// ---------- holes & handles ----------

var (
	reNoHoles  = regexp.MustCompile(`(?i)\b(?:no|without|0|zero) faucet holes?\b|\bno holes?\b`)
	reHoles    = regexp.MustCompile(`(?i)\b(\d|one|two|three|single)[- ]?(?:faucet |tap )?holes?\b`)
	reOneHole  = regexp.MustCompile(`(?i)\bmonobloc(?:k)?\b|\bsingle[- ]hole\b|\bone[- ]hole\b`)
	reHandles1 = regexp.MustCompile(`(?i)\b(?:single|one|1)[- ](?:handle|lever|control)\b|\bsingle-control\b`)
	reHandles2 = regexp.MustCompile(`(?i)\b(?:two|2|double|dual)[- ]handles?\b`)
	reHandles3 = regexp.MustCompile(`(?i)\b(?:three|3)[- ]handles?\b`)
	reLessHdl  = regexp.MustCompile(`(?i)\bless handles?\b|\bhandles? (?:sold|ordered) separately\b|\bmust order handles?\b`)
	reHdlStyle = regexp.MustCompile(`(?i)\b(lever|cross|knob|wristblade|wrist blade|joystick|paddle|loop|blade|metal lever|porcelain lever|porcelain cross|metal cross)(?:,? (?:or |and |/ ?)(lever|cross|knob|wristblade|wrist blade|joystick|paddle|loop|blade))?\s(?:handles?|hdl)\b`)
)

var numWords = map[string]string{"one": "1", "two": "2", "three": "3", "single": "1"}

func (e *ex) holesHandles(t string, conf float64) {
	if e.in.Room == "hardware" || e.in.Room == "appliances" {
		return
	}
	if !e.has("Faucet Holes") {
		switch {
		case reNoHoles.MatchString(t):
			e.add("Faucet Holes", "None", "", conf)
		default:
			if m := reHoles.FindStringSubmatch(t); m != nil {
				n := strings.ToLower(m[1])
				if v, ok := numWords[n]; ok {
					n = v
				}
				if n >= "1" && n <= "4" {
					e.add("Faucet Holes", n, "", conf)
				}
			} else if reOneHole.MatchString(t) {
				e.add("Faucet Holes", "1", "", conf)
			}
		}
	}
	if !e.has("Handles") {
		switch {
		case reLessHdl.MatchString(t):
			e.add("Handles", "Not included (order separately)", "", conf)
		case reHandles2.MatchString(t):
			e.add("Handles", "2", "", conf)
		case reHandles3.MatchString(t):
			e.add("Handles", "3", "", conf)
		case reHandles1.MatchString(t):
			e.add("Handles", "1", "", conf)
		}
	}
	if m := reHdlStyle.FindStringSubmatch(t); m != nil && !e.has("Handle Style") {
		v := titleCase(m[1])
		if m[2] != "" {
			v += " or " + titleCase(m[2])
		}
		e.add("Handle Style", v, "", conf)
	}
}

// ---------- material ----------

var materialRules = []struct {
	re  *regexp.Regexp
	val string
}{
	{regexp.MustCompile(`(?i)\bsolid brass\b|\bbrass construction\b|\bbrass body\b|\ball[- ]brass\b|\bforged brass\b|\bcast brass\b|\bmade of brass\b|\bbrass (?:faucet|spout|waste)\b`), "Solid Brass"},
	{regexp.MustCompile(`(?i)\bsolid bronze\b|\bbronze construction\b|\bcast bronze\b`), "Solid Bronze"},
	{regexp.MustCompile(`(?i)\bvitreous china\b`), "Vitreous China"},
	{regexp.MustCompile(`(?i)\bfire ?clay\b`), "Fireclay"},
	{regexp.MustCompile(`(?i)\bsilgranit\b`), "SILGRANIT granite composite"},
	{regexp.MustCompile(`(?i)\b(?:granite|quartz) composite\b`), "Composite Granite"},
	{regexp.MustCompile(`(?i)\benamel(?:ed|led) cast iron\b`), "Enameled Cast Iron"},
	{regexp.MustCompile(`(?i)\bcast iron\b`), "Cast Iron"},
	{regexp.MustCompile(`(?i)\bacrylic\b`), "Acrylic"},
	{regexp.MustCompile(`(?i)\bsolid surface\b`), "Solid Surface"},
	{regexp.MustCompile(`(?i)\bstone resin\b|\bresin\b`), "Stone Resin"},
	{regexp.MustCompile(`(?i)\b(?:\d{2}[- ]?gauge|t-?304|304|316|18/10|18/8) (?:series )?stainless(?: steel)?\b|\bstainless steel (?:sink|construction|body|bowl|tub|basin)\b|\bmade of stainless\b|\bstainless steel,? satin\b`), "Stainless Steel"},
	{regexp.MustCompile(`(?i)\b(?:made of|high[- ]quality|sanitary|fine|duravit) (?:\w+ )?ceramic\b|\bceramic (?:sink|basin|bowl|toilet|bidet|lid|material|washbasin)\b`), "Ceramic"},
	{regexp.MustCompile(`(?i)\b(?:hammered|solid) copper\b|\bcopper sink\b`), "Copper"},
	{regexp.MustCompile(`(?i)\bdie-?cast zinc\b|\bzinc alloy\b|\bzinc\b`), "Zinc"},
	{regexp.MustCompile(`(?i)\bporcelain (?:sink|basin|lavatory|enamel)\b`), "Porcelain"},
	{regexp.MustCompile(`(?i)\b(?:carrara |natural )?marble\b`), "Marble"},
	{regexp.MustCompile(`(?i)\btempered glass\b`), "Tempered Glass"},
}

var reGauge = regexp.MustCompile(`(?i)\b(\d{2})[- ]?gauge\b`)

func (e *ex) material(t string, conf float64) {
	if !e.has("Material") {
		for _, r := range materialRules {
			if r.re.MatchString(t) {
				if r.val == "Stainless Steel" && (strings.Contains(e.in.Category, "Faucet") || e.in.Room == "hardware") {
					continue // "Brushed Stainless steel" is a finish on a faucet
				}
				e.add("Material", r.val, "", conf)
				break
			}
		}
		if !e.has("Material") && (strings.Contains(e.in.Category, "Sink") || strings.Contains(e.in.Category, "Kitchen Accessories")) && regexp.MustCompile(`(?i)\bstainless steel\b`).MatchString(t) {
			e.add("Material", "Stainless Steel", "", conf-0.1)
		}
	}
	if m := reGauge.FindStringSubmatch(t); m != nil && !e.has("Gauge") {
		if g, _ := strconv.Atoi(m[1]); g >= 14 && g <= 22 {
			e.add("Gauge", m[1]+"-gauge", "", conf)
		}
	}
}

// ---------- misc ----------

var certRules = []struct {
	re  *regexp.Regexp
	val string
}{
	{regexp.MustCompile(`\bcUPC\b|\bc-?UPC\b|\bCUPC\b`), "cUPC"},
	{regexp.MustCompile(`\bUPC\b`), "UPC"},
	{regexp.MustCompile(`(?i)\bwatersense\b`), "WaterSense"},
	{regexp.MustCompile(`\bCSA\b`), "CSA"},
	{regexp.MustCompile(`\bNSF(?:/ANSI)?(?:\s?(?:61|372|42|53))?\b`), "NSF"},
	{regexp.MustCompile(`(?i)\bcal ?green\b`), "CALGreen"},
	{regexp.MustCompile(`\bIAPMO\b`), "IAPMO"},
	{regexp.MustCompile(`\bASME A112\.[\d.]+\d`), ""},
	{regexp.MustCompile(`\bASSE \d{4}\b`), ""},
	{regexp.MustCompile(`(?i)\bAB ?1953\b`), "AB 1953"},
	{regexp.MustCompile(`(?i)\blead[- ]free\b`), "Lead-free"},
	{regexp.MustCompile(`\bUL\b(?: listed)?`), "UL"},
	{regexp.MustCompile(`\bETL\b`), "ETL"},
	{regexp.MustCompile(`\bCEC\b`), "CEC"},
	{regexp.MustCompile(`(?i)\bbuy american\b|\bBAA compliant\b`), "Buy American Act"},
	{regexp.MustCompile(`(?i)\benergy star\b`), "ENERGY STAR"},
	{regexp.MustCompile(`\bANSI (?:Z124|A117)[\d.]*`), ""},
	{regexp.MustCompile(`\bADA\b`), "ADA"},
}

var (
	reDesigner = regexp.MustCompile(`[Dd]esign(?:ed)? by ([A-Z][\w.'-]+(?: (?:[A-Z][\w.'-]+|de|van|von|&))?(?: [A-Z][\w.'-]+)?)`)
	reSeriesNo = regexp.MustCompile(`(?i)\bseries:\s?((?:\d{2,4}[A-Z]?(?:,\s?|\s?&\s?|\s?/\s?)?){1,4})`)
	reValve    = regexp.MustCompile(`(?i)\b(thermostatic|pressure[- ]balanc(?:e|ed|ing))\b`)
	reCart     = regexp.MustCompile(`(?i)\b(ceramic(?: disc)? cartridge|ceramic disc|quaturn|compression cartridge|progressive cartridge|diamond seal|washerless cartridge)\b`)
	reOverNo   = regexp.MustCompile(`(?i)\bwithout overflow\b|\bw/o overflow\b|\bno overflow\b|\bnon-?overflow\b`)
	reOverYes  = regexp.MustCompile(`(?i)\bwith overflow\b|\boverflow\b`)
	reDrainYes = regexp.MustCompile(`(?i)\bdrain included\b|\bincludes drain\b|\bdrain assembly included\b|\bwith (?:pop-?up )?drain\b|\bincluded drain\b`)
	reDrainNo  = regexp.MustCompile(`(?i)\bdrain not included\b|\bwithout drain\b|\bdrain sold separately\b|\bless drain\b|\bdrain(?:s)? not included\b`)
	reDrainLoc = regexp.MustCompile(`(?i)\b(rear|center|centre|offset|left|right|end|front|reversible|corner)[- ](?:positioned |located |hand )?drain\b`)
	reBowls    = regexp.MustCompile(`(?i)\b(single|double|triple|equal double|low divide|super single|1-3/4|1-1/2|60/40|50/50) ?bowls?\b`)
	reSprays   = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|\d)[- ](?:spray )?(?:functions?|settings?|spray modes?)\b`)
	reCapacity = regexp.MustCompile(`(?i)\b(\d{2,3})\s?(?:gal(?:lons?)?)\b`)
	reWeight   = regexp.MustCompile(`(?i)\b(?:product |ship(?:ping)? |net |unit )?weight[:]?\s?(\d{1,3}(?:\.\d)?)\s?(lbs?|kg)\b`)
	reMadeIn   = regexp.MustCompile(`(?i)\bmade in (?:the )?([A-Z][a-z]+(?: and [A-Z][a-z]+)?|USA|U\.S\.A\.|UK|EU)\b`)
	reWarranty = regexp.MustCompile(`(?i)\b((?:limited )?lifetime(?: limited)? warranty|\d+[- ]year(?: limited)? warranty)\b`)
	reSpoutTyp = regexp.MustCompile(`(?i)\b(gooseneck|high[- ]arc|low[- ]arc|swivel|articulating|waterfall|straight nozzle|extended) (?:spout|nozzle|outlet)?`)
	reSprayer  = regexp.MustCompile(`(?i)\b(pull-?down|pull-?out|side spray|pre-?rinse) (?:spray|sprayer|spout|faucet|wand)?`)
	reDeckThk  = regexp.MustCompile(`(?i)accommodates up to (\d[\d\-/ ]*")\s?deck`)
)

func (e *ex) misc(t string, conf float64) {
	var certs []string
	seen := map[string]bool{}
	hasCUPC := false
	for _, r := range certRules {
		m := r.re.FindString(t)
		if m == "" {
			continue
		}
		v := r.val
		if v == "" {
			v = strings.TrimSpace(m)
		}
		if v == "cUPC" {
			hasCUPC = true
		}
		if v == "UPC" && hasCUPC {
			continue
		}
		if v == "ADA" {
			if !e.has("ADA Compliant") {
				e.add("ADA Compliant", "Yes", "", conf)
			}
			continue
		}
		if !seen[v] {
			seen[v] = true
			certs = append(certs, v)
		}
	}
	if len(certs) > 0 && !e.has("Certifications") {
		if len(certs) > 6 {
			certs = certs[:6]
		}
		e.add("Certifications", strings.Join(certs, ", "), "", conf)
	}
	if m := reDesigner.FindStringSubmatch(t); m != nil {
		d := strings.TrimRight(m[1], ".,")
		if !strings.EqualFold(d, e.in.BrandName) && !strings.Contains(strings.ToLower(e.in.BrandName), strings.ToLower(d)) {
			e.add("Designer", d, "", conf)
		}
	}
	if m := reSeriesNo.FindStringSubmatch(t); m != nil && e.in.Brand == "CHI" {
		e.add("Series", strings.TrimRight(strings.TrimSpace(m[1]), ",&/ "), "", conf)
	}
	if m := reValve.FindStringSubmatch(t); m != nil && (strings.Contains(e.in.Category, "Trim") || strings.Contains(e.in.Category, "Valve") || strings.Contains(e.in.Category, "Shower") || strings.Contains(e.in.Category, "Tub")) {
		v := titleCase(m[1])
		if strings.HasPrefix(v, "Pressure") {
			v = "Pressure Balance"
		}
		e.add("Valve Type", v, "", conf)
	}
	if m := reCart.FindStringSubmatch(t); m != nil {
		v := strings.ToLower(m[1])
		switch {
		case strings.HasPrefix(v, "ceramic"):
			v = "Ceramic Disc"
		case v == "quaturn":
			v = "Quaturn Compression"
		case v == "diamond seal":
			v = "DIAMOND Seal (ceramic)"
		default:
			v = titleCase(strings.TrimSuffix(v, " cartridge"))
		}
		e.add("Cartridge", v, "", conf)
	}
	if e.in.Room != "hardware" {
		switch {
		case reOverNo.MatchString(t):
			e.add("Overflow", "No", "", conf)
		case reOverYes.MatchString(t) && (strings.Contains(e.in.Category, "Sink") || strings.Contains(e.in.Category, "Bathtub")):
			e.add("Overflow", "Yes", "", conf)
		}
		switch {
		case reDrainNo.MatchString(t):
			e.add("Drain Included", "No", "", conf)
		case reDrainYes.MatchString(t):
			e.add("Drain Included", "Yes", "", conf)
		}
		if m := reDrainLoc.FindStringSubmatch(t); m != nil {
			e.add("Drain Location", titleCase(m[1]), "", conf)
		}
		if m := reBowls.FindStringSubmatch(t); m != nil && strings.Contains(e.in.Category, "Sink") {
			e.add("Bowls", titleCase(m[1]), "", conf)
		}
		if m := reSprays.FindStringSubmatch(t); m != nil && strings.Contains(e.in.Category, "Shower") {
			n := strings.ToLower(m[1])
			for w, v := range map[string]string{"one": "1", "two": "2", "three": "3", "four": "4", "five": "5", "six": "6"} {
				if n == w {
					n = v
				}
			}
			e.add("Spray Settings", n, "", conf)
		}
		if m := reCapacity.FindStringSubmatch(t); m != nil && strings.Contains(e.in.Category, "Bathtub") {
			if g, _ := strconv.Atoi(m[1]); g >= 15 && g <= 250 {
				e.add("Capacity", m[1]+" gal", "gal", conf)
			}
		}
		if m := reSpoutTyp.FindStringSubmatch(t); m != nil && strings.Contains(e.in.Category, "Faucet") {
			e.add("Spout Type", titleCase(strings.ReplaceAll(m[1], " ", "-")), "", conf)
		}
		if m := reSprayer.FindStringSubmatch(t); m != nil && strings.Contains(e.in.Category, "Faucet") {
			e.add("Sprayer", titleCase(strings.ReplaceAll(strings.ToLower(m[1]), " ", "-")), "", conf)
		}
	}
	if m := reWeight.FindStringSubmatch(t); m != nil {
		e.add("Weight", m[1]+" "+strings.ToLower(m[2]), strings.ToLower(m[2]), conf)
	}
	if m := reMadeIn.FindStringSubmatch(t); m != nil {
		e.add("Made In", m[1], "", conf)
	}
	if m := reWarranty.FindStringSubmatch(t); m != nil {
		e.add("Warranty", titleCase(m[1]), "", conf)
	}
}

// ---------- door & cabinet hardware ----------

var (
	reFunction = regexp.MustCompile(`(?i)\b(sectional privacy|full dummy|half dummy|single dummy|double dummy|single cylinder|double cylinder|keyed entry|passage|privacy|dummy|entry|entrance|storeroom|classroom|interconnect(?:ed)?|interior trim|exterior trim)\b`)
	reHanding  = regexp.MustCompile(`(?i)\b(right \(left\) hand|left \(right\) hand|right hand|left hand|non-handed|reversible|RH|LH)\b`)
	reKeying   = regexp.MustCompile(`(?i)\b(keyed alike|keyed different|smartkey|single keyed|double keyed)\b`)
)

func (e *ex) hardware() {
	if e.in.Room != "hardware" {
		return
	}
	if m := reFunction.FindStringSubmatch(e.own); m != nil {
		e.add("Function", titleCase(m[1]), "", 0.85)
	}
	if m := reHanding.FindStringSubmatch(e.own); m != nil {
		v := m[1]
		switch strings.ToUpper(v) {
		case "RH":
			v = "Right Hand"
		case "LH":
			v = "Left Hand"
		default:
			v = titleCase(v)
		}
		e.add("Handing", v, "", 0.8)
	}
	if m := reKeying.FindStringSubmatch(e.own); m != nil {
		e.add("Keying", titleCase(m[1]), "", 0.8)
	}
}

// ---------- Blanco rows: "443224 LIVEN 25 Single, SILGRANIT, Volcano Gray $742 63" ----------

var reBlancoRow = regexp.MustCompile(`^\s*\d{6}\s+([A-Z][A-Z]{2,}(?: [A-Z]{2,})?)\b(?:\s+(\d{2}))?`)

func (e *ex) blanco() {
	if e.in.Brand != "BLA" {
		return
	}
	for _, l := range e.in.Own {
		m := reBlancoRow.FindStringSubmatch(Norm(l))
		if m == nil {
			continue
		}
		if m[2] != "" {
			if n, _ := strconv.Atoi(m[2]); n >= 12 && n <= 48 {
				e.add("Nominal Size", m[2]+`"`, "in", 0.8)
			}
		}
		if bm := regexp.MustCompile(`(?i)\b(single|double|equal double|low divide|super single|1-3/4|1-1/2|bar|reversible)\b`).FindStringSubmatch(l); bm != nil && !e.has("Bowls") {
			e.add("Bowls", titleCase(bm[1]), "", 0.8)
		}
		break
	}
}

// ---------- availability ----------

var (
	reStock   = regexp.MustCompile(`(?i)\b(special order|in stock|quick ?ship|ready to ship|in-stock for immediate shipment|made to order)\b`)
	reLead    = regexp.MustCompile(`(?i)\b(?:lead time|ships? (?:in|within))[:]?\s?(\d+(?:\s?-\s?\d+)?\s?(?:business )?(?:days?|weeks?|wks?))\b`)
	reStockCo = regexp.MustCompile(`(?i)\bStock\s+\d{2,5}\.\d\d\b`)
)

func (e *ex) availability() {
	stock, special := 0, 0
	for _, l := range e.in.Own {
		if reStockCo.MatchString(l) {
			stock++
		}
		if strings.Contains(strings.ToLower(l), "special order") {
			special++
		}
	}
	switch {
	case stock > 0 && special == 0:
		e.add("Availability", "Stock item", "", 0.85)
	case special > 0 && stock == 0:
		e.add("Availability", "Special order", "", 0.85)
	case stock > 0 && special > 0:
		e.add("Availability", "Stock / special order by SKU", "", 0.85)
	}
	if m := reStock.FindStringSubmatch(e.own); m != nil && !e.has("Availability") {
		e.add("Availability", titleCase(m[1]), "", 0.8)
	}
	if m := reLead.FindStringSubmatch(e.own + " " + e.text); m != nil {
		e.add("Lead Time", strings.ToLower(m[1]), "", 0.7)
	}
}

// ---------- works with ----------

var worksRules = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bfor use with ((?:[A-Za-z0-9][\w\-/.&#]*,?\s?(?:or |and |& )?){1,10})`),
	regexp.MustCompile(`(?i)\bcompatible with ((?:[A-Za-z0-9][\w\-/.&#]*,?\s?(?:or |and |& )?){1,10})`),
	regexp.MustCompile(`(?i)\brequires rough(?:-in)? valve[:]?\s?((?:[A-Z0-9][\w\-/.&]*,?\s?(?:or |and |& )?){1,4})`),
	regexp.MustCompile(`(?i)\bfor series[:]?\s?((?:[A-Z0-9][\w\-/]*,?\s?(?:or |and |& )?){1,10})`),
	regexp.MustCompile(`(?i)\b(?:use|pair|order) with (?:flange |rough |valve |trim )?([A-Z0-9][\w\-/.]*(?: \(above\))?(?:,? (?:or |and |& )?[A-Z0-9][\w\-/.]*){0,5})`),
	regexp.MustCompile(`(?i)\bfits ((?:[A-Za-z0-9][\w\-/.&#]*(?:,? (?:or |and |& )?)?){1,6})`),
}

var reWorksCut = regexp.MustCompile(`(?i)\s(standard|special|premium|see|page|pages)\b.*$|\s\d{2,5}\.\d\d\b.*$|\s\d{2,5}$|[\s,.;:]+$`)
var reCapsJunk = regexp.MustCompile(`\b[A-Z]{5,}\b`)
var reRequires = regexp.MustCompile(`(?i)\bmust order (rough|handles?|rough valve|valve|trim|spout)s?(?: and (handles?|rough|trim))? separately\b`)

func (e *ex) worksWith() {
	type hit struct {
		pos int
		val string
	}
	var hits []hit
	seen := map[string]bool{}
	src := e.text
	for _, re := range worksRules {
		for _, loc := range re.FindAllStringSubmatchIndex(src, 3) {
			m := []string{src[loc[0]:loc[1]], src[loc[2]:loc[3]]}
			v := strings.TrimSpace(reWorksCut.ReplaceAllString(m[1], ""))
			v = strings.TrimRight(v, " ,.;:-")
			if len(v) < 3 || len(words(v)) > 14 || junkRatio(v) > 0.25 || reCapsJunk.MatchString(v) {
				continue
			}
			for _, s := range e.in.SKUs {
				if strings.EqualFold(v, s) {
					v = ""
				}
			}
			if v == "" {
				continue
			}
			if r := v[len(v)-1]; len(v) > 2 && v[len(v)-2] == ' ' && r >= 'a' && r <= 'z' {
				v = strings.TrimSpace(v[:len(v)-2]) // "rough o": a cut word
			}
			k := strings.ToLower(v)
			if !seen[k] {
				seen[k] = true
				hits = append(hits, hit{loc[0], v})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	var vals []string
	for _, h := range hits {
		vals = append(vals, h.val)
	}
	if len(vals) > 0 {
		if len(vals) > 3 {
			vals = vals[:3]
		}
		v := strings.Join(vals, "; ")
		if len(v) > 160 {
			v = v[:160]
			if i := strings.LastIndex(v, " "); i > 100 {
				v = v[:i]
			}
		}
		e.add("Works With", v, "", 0.7)
	}
	var req []string
	for _, m := range reRequires.FindAllStringSubmatch(src, 3) {
		for _, w := range []string{m[1], m[2]} {
			switch strings.TrimSuffix(strings.ToLower(w), "s") {
			case "rough", "rough valve", "valve":
				req = appendUniq(req, "Rough-in valve (sold separately)")
			case "handle":
				req = appendUniq(req, "Handles (sold separately)")
			case "trim":
				req = appendUniq(req, "Trim (sold separately)")
			case "spout":
				req = appendUniq(req, "Spout (sold separately)")
			}
		}
	}
	if len(req) > 0 {
		e.add("Requires", strings.Join(req, "; "), "", 0.8)
	}
}

func appendUniq(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

// ---------- collection ----------

func (e *ex) collection() (string, float64) {
	for _, t := range e.in.TOC {
		if c := Collection(t, e.in.BrandName); c != "" {
			return c, 0.8
		}
	}
	if c := Collection(e.in.Heading, e.in.BrandName); c != "" {
		return c, 0.7
	}
	// Duravit/Blanco style: the block's title line names the line ("DuraSquare Vessel Sink")
	if e.in.Brand == "DRV" {
		for _, l := range e.in.Block {
			if strings.Contains(l, "Design by") || strings.Contains(l, "design by") {
				if c := Collection(l, e.in.BrandName); c != "" {
					return c, 0.75
				}
			}
		}
	}
	if e.in.Brand == "BLA" {
		for _, l := range e.in.Own {
			if m := reBlancoRow.FindStringSubmatch(Norm(l)); m != nil {
				if c := Collection(m[1], e.in.BrandName); c != "" {
					return c, 0.8
				}
			}
		}
	}
	if c := collectionInText(e.own+" "+e.text, e.in.BrandName); c != "" {
		return c, 0.6
	}
	if s, ok := e.facts["Series"]; ok && e.in.Brand == "CHI" {
		return strings.ReplaceAll(strings.ReplaceAll(s.Value, ", ", "/"), " & ", "/") + " Series", 0.6
	}
	return "", 0
}

// ---------- finishes ----------

func (e *ex) finishes() []string {
	out := blockFinishes(e.own)
	for _, f := range blockFinishes(e.text) {
		out = appendUniq(out, f)
	}
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// rowFinish returns the single finish the family's own rows name, "" when none or several.
func rowFinish(own []string) string {
	var got string
	for _, l := range own {
		for _, f := range Finishes(Norm(l)) {
			if got != "" && got != f {
				return ""
			}
			got = f
		}
	}
	return got
}

// LineFinish returns the one finish named on a single SKU row ("TS-30-30 Wall Tub Spout
// polished chrome 335"), "" when none or more than one.
func LineFinish(line string) string {
	fs := Finishes(Norm(line))
	if len(fs) == 1 {
		return fs[0]
	}
	return ""
}
