// Package catinfo mines OCR'd vendor catalog pages (price books, spec books, product
// catalogs) for product information beyond SKU and price: dimensions, flow rates, mounting,
// holes and handles, material, certifications, collection names, feature bullets, available
// finishes and "works with" relations. Rules are regular expressions over normalized text;
// every rule is covered by a table test using a real OCR line from the database.
package catinfo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// fractions and marks the OCR reads as single glyphs
var glyphs = strings.NewReplacer(
	"½", " 1/2", "¼", " 1/4", "¾", " 3/4", "⅛", " 1/8", "⅜", " 3/8", "⅝", " 5/8", "⅞", " 7/8",
	"⅓", " 1/3", "⅔", " 2/3", "⅕", " 1/5", "⅙", " 1/6", "⅒", " 1/10", "⅐", " 1/7", "⅑", " 1/9", "⅖", " 2/5", "⅗", " 3/5", "⅘", " 4/5", "⅚", " 5/6",
	"”", `"`, "“", `"`, "″", `"`, "''", `"`, "’", "'", "‘", "'", "′", "'",
	"–", "-", "—", "-", "‐", "-", "×", "x", "Ø", "", "ø", "", "®", "", "™", "", "©", "", " ", " ",
)

var (
	reSpaces  = regexp.MustCompile(`[ \t]+`)
	reIHole   = regexp.MustCompile(`(^|[\s(])[IlL](-hole)\b`)
	reGom     = regexp.MustCompile(`(?i)(\d\s*)g[o0]m\b`)
	rePipeOne = regexp.MustCompile(`(^|[\s:(])[|lI]([.\d]\d*\s*(?:gpm|gpf|"|/))`)
)

// Norm normalizes OCR text: single inch marks, ASCII fractions, plain dashes, no symbols,
// collapsed spaces; "1.8 gom" and "|.8 gpm" become "1.8 gpm". Line breaks are kept.
func Norm(s string) string {
	s = glyphs.Replace(s)
	s = reGom.ReplaceAllString(s, "${1}gpm")
	s = rePipeOne.ReplaceAllString(s, "${1}1${2}")
	s = reIHole.ReplaceAllString(s, "${1}1${2}")
	return reSpaces.ReplaceAllString(s, " ")
}

// Flat joins lines with spaces for rules that may span a line break.
func Flat(lines []string) string {
	return strings.TrimSpace(reSpaces.ReplaceAllString(strings.Join(lines, " "), " "))
}

// Dim is a printed length.
type Dim struct {
	Whole, Num, Den int
	Dec             float64 // when printed as a decimal (2.375) or in mm
	IsDec           bool
	Unit            string // "in" or "mm"
}

// Inches returns the value in inches.
func (d Dim) Inches() float64 {
	switch {
	case d.Unit == "mm":
		return d.Dec / 25.4
	case d.IsDec:
		return d.Dec
	case d.Den > 0:
		return float64(d.Whole) + float64(d.Num)/float64(d.Den)
	}
	return float64(d.Whole)
}

// String prints the way a spec sheet does: 9-1/8", 2.375", 350 mm.
func (d Dim) String() string {
	switch {
	case d.Unit == "mm":
		return strconv.FormatFloat(d.Dec, 'f', -1, 64) + " mm"
	case d.IsDec:
		return strconv.FormatFloat(d.Dec, 'f', -1, 64) + `"`
	case d.Den > 0 && d.Whole > 0:
		return fmt.Sprintf(`%d-%d/%d"`, d.Whole, d.Num, d.Den)
	case d.Den > 0:
		return fmt.Sprintf(`%d/%d"`, d.Num, d.Den)
	}
	return fmt.Sprintf(`%d"`, d.Whole)
}

// dimPat matches one printed length: 9 1/8", 9-1/8", 91/8" (the OCR drops the space; the
// regexp backtracks to whole 9 + 1/8), 1/2", 2.375", 16"W, 350 mm, 9.5in. "in" as a unit is
// accepted only glued to the number or written "in." so "available in 2" is not a length.
const dimPat = `(\d{1,3}(?:\.\d{1,3})?)(?:[ -]?(\d{1,2})/(\d{1,2}))?(?:\s?"|in\.|in\b|\s?inch(?:es)?\b|\s?mm\b)|(\d{1,2})/(\d{1,2})\s?"`

var (
	reDim     = regexp.MustCompile(dimPat)
	reBadFrac = regexp.MustCompile(`\d\s?[%¥]|[%¥]\s?"`)
)

var okDen = map[int]bool{2: true, 3: true, 4: true, 5: true, 8: true, 10: true, 16: true, 32: true}

// parseDim turns a dimPat submatch into a Dim; ok is false for impossible reads.
func parseDim(m []string) (Dim, bool) {
	d := Dim{Unit: "in"}
	if strings.HasSuffix(strings.TrimSpace(strings.ToLower(m[0])), "mm") {
		d.Unit = "mm"
	}
	if m[4] != "" { // bare fraction 1/2"
		n, _ := strconv.Atoi(m[4])
		dn, _ := strconv.Atoi(m[5])
		if n <= 0 || n >= dn || !okDen[dn] {
			return d, false
		}
		d.Num, d.Den = n, dn
		return d, true
	}
	if strings.Contains(m[1], ".") {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil || f <= 0 || m[2] != "" {
			return d, false
		}
		d.Dec, d.IsDec = f, true
		if d.Unit == "mm" {
			return d, f < 10000
		}
		return d, f < 400
	}
	w, _ := strconv.Atoi(m[1])
	if d.Unit == "mm" {
		d.Dec, d.IsDec = float64(w), true
		return d, w > 0 && m[2] == ""
	}
	if m[2] == "" {
		d.Whole = w
		return d, w > 0 && w < 400
	}
	n, _ := strconv.Atoi(m[2])
	dn, _ := strconv.Atoi(m[3])
	if !okDen[dn] || n <= 0 || n >= dn {
		return d, false
	}
	glued := len(m[1]) < len(m[0]) && m[0][len(m[1])] != ' ' && m[0][len(m[1])] != '-'
	if glued && w >= 100 && dn == 16 && (w%10)*10+n < 16 {
		w, n = w/10, (w%10)*10+n // "1115/16" is 11 15/16, not 111 5/16
	}
	d.Whole, d.Num, d.Den = w, n, dn
	return d, w < 400
}

type dimHit struct {
	Dim
	Start, End int
}

// findDims returns every well-formed dimension in s in order with its byte offsets.
func findDims(s string) []dimHit {
	var out []dimHit
	for _, loc := range reDim.FindAllStringSubmatchIndex(s, -1) {
		m := make([]string, 6)
		for i := 0; i < 6; i++ {
			if loc[2*i] >= 0 {
				m[i] = s[loc[2*i]:loc[2*i+1]]
			}
		}
		if reBadFrac.MatchString(s[loc[0]:min(len(s), loc[1]+2)]) || strings.ContainsAny(s[max(0, loc[0]-2):loc[0]], "%¥") {
			continue
		}
		if loc[0] > 0 && (s[loc[0]-1] == '.' || s[loc[0]-1] == '/' || s[loc[0]-1] == '-' && loc[0] > 1 && s[loc[0]-2] >= '0' && s[loc[0]-2] <= '9') {
			continue // the tail of a model number or a page range: "9130-37", "572-574"
		}
		d, ok := parseDim(m)
		if !ok {
			continue
		}
		out = append(out, dimHit{d, loc[0], loc[1]})
	}
	return out
}

// titleCase upper-cases the first letter of every word longer than two letters.
func titleCase(s string) string {
	w := strings.Fields(strings.ToLower(s))
	for i, x := range w {
		if len(x) > 0 && (i == 0 || len(x) > 2) {
			w[i] = strings.ToUpper(x[:1]) + x[1:]
		}
	}
	return strings.Join(w, " ")
}

// sentenceCase lowers an ALL-CAPS line and capitalizes its first letter.
func sentenceCase(s string) string {
	if strings.ToUpper(s) != s || len(s) == 0 {
		return s
	}
	l := strings.ToLower(s)
	return strings.ToUpper(l[:1]) + l[1:]
}

// words splits on spaces and trims punctuation from each word.
func words(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		if w = strings.Trim(w, ",.;:()[]|"); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// junkRatio is the share of tokens that are neither words nor numbers.
func junkRatio(s string) float64 {
	ws := words(s)
	if len(ws) == 0 {
		return 1
	}
	junk := 0
	for _, w := range ws {
		letters, digits, other := 0, 0, 0
		for _, r := range w {
			switch {
			case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
				letters++
			case r >= '0' && r <= '9':
				digits++
			case strings.ContainsRune(`-'/&."@#%$+=°`, r):
			default:
				other++
			}
		}
		if other > 0 || letters == 1 && digits == 0 && len(w) == 1 && !strings.ContainsAny(w, "aAI&") {
			junk++
		}
	}
	return float64(junk) / float64(len(ws))
}
