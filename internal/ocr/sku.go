package ocr

import (
	"regexp"
	"strings"
)

// Tokens that pass the SKU shape test but are measurements, standards or references printed
// beside products in a price book. Each one became a product ("4.5L/MIN", "A112.1016/ASSE",
// "1016/CSA" from Brizo's flow-rate footnotes; "120Z" from Alno's weight column; "2700K/4000K"
// from Robern's colour temperatures) before this filter existed.
var junkSKU = []*regexp.Regexp{
	// rates and quantities: 4.5L/MIN, 8.8L/MIH (OCR of MIN), 0.3MG/L, 1.2L/MIN, 50/BOX, 2.5/T1, 12OZ/120Z
	regexp.MustCompile(`^[\d.]+L?/(MIN|MIH|SEC|HR?|L|BOX|PK|CS|T\d)$`),
	regexp.MustCompile(`^[\d.]+(MG|ML|OZ|0Z|LB|LBS|KG|GAL|GPM|GPF|LPM|PSI|KPA|BAR|KW|AMPS?)(/[A-Z]+)?$`),
	// plumbing and electrical standards: A112.18.1, A112.1016/ASSE, 1016/CSA, 2012/CSA, NSF/ANSI372, B125.1
	regexp.MustCompile(`^(A|AL)112\.`),
	regexp.MustCompile(`(^|[/.-])(ASSE|CSA|ANSI|NSF|ASME|IAPMO|UPC|CUPC|ADA|UL|CSAUS)(\d|/|$)`),
	// magnification, colour temperature, voltage: 1X/10X, 2700K/4000K, KELVIN/4000, 120V/60HZ
	regexp.MustCompile(`^\d+X/\d+X$`),
	regexp.MustCompile(`^\d{4}K(/\d{4}K)?$`),
	regexp.MustCompile(`^KELVIN/`),
	regexp.MustCompile(`^\d+V/\d+HZ$`),
	// a phone number (Robern's 800-877-2376 became a $97 product)
	regexp.MustCompile(`^1?-?\d{3}-\d{3}-\d{4}$`),
	// a word and a number joined by a slash is a label, not a model: AMPS/0.75, MODEL/COLOR1, W/6701 ("with 6701")
	regexp.MustCompile(`^[A-Z]{1,6}/[\d.]+$`),
	regexp.MustCompile(`^MODEL/`),
	// two or three finish codes with a slash: RENT/LENT, LH/RH, 6F/7F/8F, CS/BTB/GBTB
	regexp.MustCompile(`^\d?[A-Z]{1,4}\d?/\d?[A-Z]{1,4}\d?(/[A-Z0-9]{1,4})?$`),
}

// JunkSKU reports whether a SKU-shaped token is really a measurement, a standard or a label.
func JunkSKU(t string) bool {
	u := strings.Trim(strings.ToUpper(t), ",;:()[]")
	for _, re := range junkSKU {
		if re.MatchString(u) {
			return true
		}
	}
	return false
}

// IsReference reports whether a word is printed in parentheses: "(US10B)", "(85354)," — a finish
// code, a cross-reference or a component mention, never the line's own model number.
func IsReference(raw string) bool {
	t := strings.TrimRight(strings.TrimSpace(raw), ",;:.")
	return len(t) >= 3 && t[0] == '(' && t[len(t)-1] == ')'
}
