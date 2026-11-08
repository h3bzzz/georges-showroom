package specs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fact is one typed value mined from a sheet.
type Fact struct {
	Value  string  `json:"value"`
	Unit   string  `json:"unit,omitempty"`
	Source string  `json:"source"`
	Conf   float32 `json:"conf,omitempty"`
}

var (
	reWS       = regexp.MustCompile(`[ \t\x{00a0}]+`)
	reInches   = `(\d+(?:[.,]\d+)?(?:\s*-\s*\d+/\d+)?|\d+/\d+)\s*(?:''|"|”|in\.?|inch(?:es)?|-inch)`
	reFlow     = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*gpm`)
	reSpoutH   = regexp.MustCompile(`(?i)` + reInches + `[^\n]{0,30}?spout height|spout height[^\n]{0,20}?[:=]?\s*` + reInches)
	reSpoutR   = regexp.MustCompile(`(?i)` + reInches + `[^\n]{0,30}?spout reach|spout reach[^\n]{0,20}?[:=]?\s*` + reInches)
	reOverallH = regexp.MustCompile(`(?i)overall height[^\n]{0,12}?[:=]?\s*` + reInches + `|` + reInches + `\s*overall height`)
	reCenters  = regexp.MustCompile(`(?i)` + reInches + `\s*(?:spout\s+)?(?:center to center|centers?|c-c|cc\b|widespread)|(?:center to center|centers)[:\s]*` + reInches)
	reWallC    = regexp.MustCompile(`(?i)` + reInches + `\s*wall to center`)
	reBowlD    = regexp.MustCompile(`(?i)bowl depth[^\n]{0,8}?[:=]?\s*(\d+(?:[.,]\d+)?)\s*(?:''|"|in)`)
	reBowlL    = regexp.MustCompile(`(?i)bowl length[^\n]{0,24}?[:=]?\s*(\d+(?:[.,]\d+)?)\s*(?:''|"|in)`)
	reBowlW    = regexp.MustCompile(`(?i)bowl width[^\n]{0,24}?[:=]?\s*(\d+(?:[.,]\d+)?)\s*(?:''|"|in)`)
	reCabinet  = regexp.MustCompile(`(?i)min(?:imum)?\.? cabinet size[:\s]*(\d+)\s*(?:''|"|in)`)
	reCutL     = regexp.MustCompile(`(?i)cut-?out length[:\s]*(\d+(?:[.,]\d+)?)\s*(?:''|"|in)`)
	reCutW     = regexp.MustCompile(`(?i)cut-?out width[:\s]*(\d+(?:[.,]\d+)?)\s*(?:''|"|in)`)
	rePressure = regexp.MustCompile(`(?i)(\d+)\s*psi\s*max`)
	reCartMM   = regexp.MustCompile(`(?i)(\d+)\s*mm\s*(?:ceramic\s*)?(?:disc\s*)?(?:valve\s*)?cartridge|ceramic disc\s*(\d+)\s*mm`)
	reWarranty = regexp.MustCompile(`(?i)(limited lifetime|lifetime limited|lifetime|\d+-year limited|\d+ year limited|\d+-year)\s+(?:faucet |finish |mechanical |sink |product )?warranty`)
	reProdType = regexp.MustCompile(`(?i)product type\s*[:\-]?\s*\n?\s*([^\n]{4,110})`)
	reBullet   = regexp.MustCompile(`^\s*[•·●▪■\-\*]\s*(.+)$`)
	reFeatures = regexp.MustCompile(`(?i)^\s*features(?: & specifications| and benefits)?\s*$`)
	reStop     = regexp.MustCompile(`(?i)^\s*(codes?[ /&]|standards|specifications subject|sustainability|available colou?rs|warranty|what is included|design and planning|optional accessories|dimensions|performance specification|job name)`)
)

var standards = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`(?i)ASME\s*A112\.18\.1`), "ASME A112.18.1 / CSA B125.1"},
	{regexp.MustCompile(`(?i)\bc?UPC\b|IAPMO`), "cUPC (IAPMO)"},
	{regexp.MustCompile(`(?i)NSF/ANSI(?:/CAN)?\s*61`), "NSF/ANSI 61"},
	{regexp.MustCompile(`(?i)NSF/ANSI(?:/CAN)?\s*372|lead[- ]free`), "NSF/ANSI 372 lead-free"},
	{regexp.MustCompile(`(?i)WaterSense`), "EPA WaterSense"},
	{regexp.MustCompile(`(?i)CALGreen|California Energy Commission|\bCEC\b`), "California Energy Commission"},
	{regexp.MustCompile(`(?i)CSA\s*B45`), "CSA B45"},
	{regexp.MustCompile(`(?i)ASME\s*A112\.19`), "ASME A112.19"},
	{regexp.MustCompile(`(?i)\bUL\b\s*(?:listed|\d)`), "UL listed"},
	{regexp.MustCompile(`(?i)\bcsa\s*certified|\bCSA\b\s*(?:listed|9\.1)`), "CSA"},
}

// Mined is what Extract found.
type Mined struct {
	Facts   map[string]Fact
	Bullets []string
	Type    string // the maker's one-line product type, when printed
}

// Extract reads the common specification facts out of a sheet's text. Every regex is
// anchored to the vocabulary makers actually print (gpm, spout height, center to center,
// bowl depth, cut-out …), so a miss stays a miss rather than a guess.
func Extract(text string) Mined {
	m := Mined{Facts: map[string]Fact{}}
	if strings.TrimSpace(text) == "" {
		return m
	}
	t := strings.ReplaceAll(text, "\r", "")
	t = strings.ReplaceAll(t, "''", `"`)
	t = reWS.ReplaceAllString(t, " ")
	set := func(key, value, unit string, conf float32) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := m.Facts[key]; ok {
			return
		}
		m.Facts[key] = Fact{Value: value, Unit: unit, Source: "spec", Conf: conf}
	}
	first := func(re *regexp.Regexp) string {
		sm := re.FindStringSubmatch(t)
		for i := 1; i < len(sm); i++ {
			if sm[i] != "" {
				return strings.ReplaceAll(sm[i], ",", ".")
			}
		}
		return ""
	}
	if v := first(reFlow); v != "" {
		set("Flow Rate", v, "gpm", 0.9)
	}
	if v := first(reSpoutH); v != "" {
		set("Spout Height", v, "in", 0.85)
	}
	if v := first(reSpoutR); v != "" {
		set("Spout Reach", v, "in", 0.85)
	}
	if v := first(reOverallH); v != "" {
		set("Overall Height", v, "in", 0.8)
	}
	if v := first(reCenters); v != "" {
		set("Centers", v, "in", 0.75)
	}
	if v := first(reWallC); v != "" {
		set("Spout Reach", v, "in", 0.7)
	}
	if v := first(reBowlD); v != "" {
		set("Bowl Depth", v, "in", 0.9)
	}
	if v := first(reBowlL); v != "" {
		set("Bowl Length", v, "in", 0.9)
	}
	if v := first(reBowlW); v != "" {
		set("Bowl Width", v, "in", 0.9)
	}
	if v := first(reCutL); v != "" {
		set("Cut-out Length", v, "in", 0.9)
	}
	if v := first(reCutW); v != "" {
		set("Cut-out Width", v, "in", 0.9)
	}
	if v := first(reCabinet); v != "" {
		set("Minimum Cabinet", v, "in", 0.9)
	}
	if v := first(rePressure); v != "" {
		set("Max Pressure", v, "psi", 0.8)
	}
	if v := first(reCartMM); v != "" {
		set("Cartridge", v+" mm ceramic disc", "", 0.8)
	} else if regexp.MustCompile(`(?i)ceramic disc`).MatchString(t) {
		set("Cartridge", "Ceramic disc", "", 0.7)
	}
	lower := strings.ToLower(t)
	switch {
	case strings.Contains(lower, "undermount installation only"), strings.Contains(lower, "undermount only"):
		set("Mounting", "Undermount", "", 0.9)
	case strings.Contains(lower, "dual mount") || strings.Contains(lower, "dualmount"):
		set("Mounting", "Dual mount (drop-in or undermount)", "", 0.85)
	case strings.Contains(lower, "wall-mount") || strings.Contains(lower, "wall mount") || strings.Contains(lower, "wall to center"):
		set("Mounting", "Wall mount", "", 0.7)
	case strings.Contains(lower, "deck-mount") || strings.Contains(lower, "deck mount"):
		set("Mounting", "Deck mount", "", 0.7)
	case strings.Contains(lower, "drop-in") || strings.Contains(lower, "top mount") || strings.Contains(lower, "topmount"):
		set("Mounting", "Drop-in", "", 0.7)
	case strings.Contains(lower, "farmhouse") || strings.Contains(lower, "apron front") || strings.Contains(lower, "apron-front"):
		set("Mounting", "Apron front", "", 0.7)
	}
	if strings.Contains(lower, "ada") && (strings.Contains(lower, "ada compliant") || strings.Contains(lower, "ada &") || strings.Contains(lower, "a117.1")) {
		v := "Yes"
		if strings.Contains(lower, "handle only") || strings.Contains(lower, "handles only") {
			v = "Yes (handles)"
		}
		set("ADA Compliant", v, "", 0.8)
	}
	switch {
	case strings.Contains(lower, "solid brass"):
		set("Material", "Solid brass", "", 0.85)
	case strings.Contains(lower, "silgranit"):
		set("Material", "SILGRANIT granite composite", "", 0.9)
	case strings.Contains(lower, "fireclay"):
		set("Material", "Fireclay", "", 0.85)
	case strings.Contains(lower, "vitreous china"):
		set("Material", "Vitreous china", "", 0.85)
	case strings.Contains(lower, "cast iron"):
		set("Material", "Enameled cast iron", "", 0.8)
	case regexp.MustCompile(`(?i)(\d+)[- ]gauge`).MatchString(t):
		g := regexp.MustCompile(`(?i)(\d+)[- ]gauge`).FindStringSubmatch(t)[1]
		set("Material", g+"-gauge stainless steel", "", 0.8)
	case strings.Contains(lower, "stainless steel"):
		set("Material", "Stainless steel", "", 0.6)
	}
	if sm := reWarranty.FindStringSubmatch(t); sm != nil {
		w := strings.ToLower(sm[1])
		w = strings.ToUpper(w[:1]) + w[1:]
		set("Warranty", w, "", 0.7)
	}
	var certs []string
	seen := map[string]bool{}
	for _, s := range standards {
		if s.re.MatchString(t) && !seen[s.name] {
			seen[s.name] = true
			certs = append(certs, s.name)
		}
	}
	if len(certs) > 0 {
		set("Certifications", strings.Join(certs, ", "), "", 0.8)
	}
	if sm := reProdType.FindStringSubmatch(t); sm != nil {
		pt := strings.TrimSpace(sm[1])
		if i := strings.Index(pt, "Features"); i > 3 {
			pt = strings.TrimSpace(pt[:i])
		}
		if len(pt) >= 4 {
			m.Type = pt
		}
	}
	// feature bullets: the lines under a Features heading, or any bullet lines, until a
	// codes/standards block starts
	lines := strings.Split(t, "\n")
	inFeatures := false
	for _, ln := range lines {
		if reFeatures.MatchString(ln) {
			inFeatures = true
			continue
		}
		if inFeatures && reStop.MatchString(ln) {
			break
		}
		if sm := reBullet.FindStringSubmatch(ln); sm != nil {
			if b := cleanBullet(sm[1]); b != "" {
				m.Bullets = append(m.Bullets, b)
			}
		} else if inFeatures {
			b := cleanBullet(ln)
			// a features block without bullet glyphs (Newport Brass) lists one feature per line
			if b != "" && len(b) > 12 && len(b) < 140 && !strings.Contains(b, "____") {
				m.Bullets = append(m.Bullets, b)
			}
		}
		if len(m.Bullets) >= 8 {
			break
		}
	}
	return m
}

var reJunkBullet = regexp.MustCompile(`(?i)^(page \d|\d+$|www\.|http|job name|item number|section/tag|model specified|architect|engineer|contractor|date\b|\[ \]|specifications subject)`)

func cleanBullet(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, " .;,")
	if len(s) < 6 || len(s) > 160 || reJunkBullet.MatchString(s) {
		return ""
	}
	if strings.Count(s, "_") > 3 {
		return ""
	}
	return s
}

// Miner writes mined facts into families.
type Miner struct {
	Pool   *pgxpool.Pool
	Brand  string
	DryRun bool
}

// Run mines every family that has a spec sheet with text. Spec facts replace catalog facts of
// the same key (the maker's sheet beats OCR of a price book) and never touch site attributes;
// bullets fill in only when the family has none; the product type fills an empty description.
func (mn *Miner) Run(ctx context.Context) (int, error) {
	rows, err := mn.Pool.Query(ctx, `
		SELECT DISTINCT ON (f.id) f.id, f.facts, f.bullets, f.description, d.text
		FROM families f JOIN brands b ON b.id=f.brand_id JOIN documents d ON d.family_id=f.id
		WHERE d.kind='spec' AND d.text IS NOT NULL AND ($1='' OR b.code=$1)
		ORDER BY f.id, d.id`, mn.Brand)
	if err != nil {
		return 0, err
	}
	type upd struct {
		id     int64
		facts  []byte
		bul    []string
		desc   *string
		setBul bool
	}
	var ups []upd
	for rows.Next() {
		var id int64
		var facts []byte
		var bullets []string
		var desc *string
		var text string
		if err := rows.Scan(&id, &facts, &bullets, &desc, &text); err != nil {
			rows.Close()
			return 0, err
		}
		got := Extract(text)
		if len(got.Facts) == 0 && len(got.Bullets) == 0 && got.Type == "" {
			continue
		}
		cur := map[string]json.RawMessage{}
		_ = json.Unmarshal(facts, &cur)
		changed := false
		for k, f := range got.Facts {
			if old, ok := cur[k]; ok {
				var of Fact
				_ = json.Unmarshal(old, &of)
				if of.Source != "" && of.Source != "catalog" && of.Source != "spec" {
					continue // site attributes stand
				}
				if of.Source == "spec" && of.Value == f.Value {
					continue
				}
			}
			b, _ := json.Marshal(f)
			cur[k] = b
			changed = true
		}
		u := upd{id: id}
		if changed {
			u.facts, _ = json.Marshal(cur)
		}
		if len(bullets) == 0 && len(got.Bullets) > 0 {
			u.bul, u.setBul = got.Bullets, true
		}
		if (desc == nil || *desc == "") && got.Type != "" {
			d := got.Type
			u.desc = &d
		}
		if u.facts != nil || u.setBul || u.desc != nil {
			ups = append(ups, u)
		}
	}
	rows.Close()
	if mn.DryRun {
		for i, u := range ups {
			if i >= 15 {
				break
			}
			fmt.Printf("%d facts=%s bullets=%d desc=%v\n", u.id, string(u.facts), len(u.bul), u.desc != nil)
		}
		return len(ups), nil
	}
	batch := &pgx.Batch{}
	for _, u := range ups {
		switch {
		case u.facts != nil && u.setBul:
			batch.Queue(`UPDATE families SET facts=$2, bullets=$3, description=COALESCE(description,$4), updated_at=now() WHERE id=$1`, u.id, u.facts, u.bul, u.desc)
		case u.facts != nil:
			batch.Queue(`UPDATE families SET facts=$2, description=COALESCE(description,$3), updated_at=now() WHERE id=$1`, u.id, u.facts, u.desc)
		case u.setBul:
			batch.Queue(`UPDATE families SET bullets=$2, description=COALESCE(description,$3), updated_at=now() WHERE id=$1`, u.id, u.bul, u.desc)
		default:
			batch.Queue(`UPDATE families SET description=COALESCE(description,$2), updated_at=now() WHERE id=$1`, u.id, u.desc)
		}
	}
	br := mn.Pool.SendBatch(ctx, batch)
	for range ups {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	slog.Info("specs mined", "families", len(ups), "brand", mn.Brand)
	return len(ups), nil
}

// Keys lists the fact keys Extract can produce, for documentation and tests.
func Keys() []string {
	k := []string{"Flow Rate", "Spout Height", "Spout Reach", "Overall Height", "Centers", "Bowl Depth", "Bowl Length", "Bowl Width", "Cut-out Length", "Cut-out Width", "Minimum Cabinet", "Max Pressure", "Cartridge", "Mounting", "ADA Compliant", "Material", "Warranty", "Certifications"}
	sort.Strings(k)
	return k
}
