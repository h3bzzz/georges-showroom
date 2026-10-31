package catinfo

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reBulletMark   = regexp.MustCompile(`^\s*[•+«»\-*=>›·]\s*(.+)$`)
	reBulletSep    = regexp.MustCompile(`\s[+«•·]\s`)
	rePriceTok     = regexp.MustCompile(`\$\s?\d|\b\d{1,3}(?:,\d{3})+(?:\.\d\d)?\b|\b\d+\.\d\d\b`)
	reBulletJunk   = regexp.MustCompile(`(?i)\b(see page|see pages|page \d|replace (?:xx|fr|\.fr)|for ?more details|for ?additional|reference the|refer to|prices? |obsolete|visit |www\.|\.com\b|call |contact |net price|list price|msrp|quick ?ship|in stock|effective|shown (?:above|below|with|in)|pictured|conditions|orders will|will apply|terms|receipt of order|thank you|please|we're|we are|our |your )\b`)
	rePronoun      = regexp.MustCompile(`(?i)\b(we|we're|we've|our|you|your|you're|thank you|please|digits?|indicates?|e\.g\.|i\.e\.|ordered|purchase|chart|below|above|following|upgrade only)\b`)
	reImperative   = regexp.MustCompile(`(?i)^(install|insert|remove|attach|tighten|loosen|apply|place|slide|turn|push|pull|drill|order|add|use|select|specify|replace|see|note|call|contact|check|make|ensure|do not|don't|always|never|simply|these|this|that|those|example|ex|e\.g)\b`)
	reNonASCII     = regexp.MustCompile("[^\x00-\x7F°]")
	reDescWord     = regexp.MustCompile(`(?i)\b(faucets?|sinks?|toilets?|showers?|tubs?|baths?|bathroom|kitchen|basins?|mixers?|design|designed|brass|finish|finishes|collection|series|water|handles?|spouts?|cabinets?|mirrors?|doors?|hardware|lever|knob|pulls?|valve|ceramic|china|steel|stone|wood|lighting|lights?|vanity|vanities|bidet|urinal|drain|technology|style|styling|crafted|engineered|made)\b`)
	reOddFrac      = regexp.MustCompile(`\d+/(\d+)"?`)
	reFeatureHint  = regexp.MustCompile(`(?i)\b(construction|finish|finished|cartridge|cartridges|included|includes|certified|listed|compliant|resistant|standard|hardware|warranty|flow|spray|sprays|handle|handles|hose|drain|overflow|installation|mount|mounted|outlet|inlet|lever|aerated|aerator|gpm|psi|ceramic|brass|steel|china|fireclay|acrylic|insulator|support|lumbar|w/o|waste|nozzle|nozzles|diverter|valve|thermostatic|technology|swivel|reach|height|depth|width|projection|cutout|bowl|basin|gauge|coated|plated|solid|engineered|concealed|adjustable|reversible|lifetime|rated|approved|supply|connection|connections|holder|bracket|escutcheon|plate|lines|design|designed|concealed|hidden|magnetic|pull-?out|pull-?down|spout)\b`)
	reLowerStart   = regexp.MustCompile(`^[a-z(]`)
	reInlineBullet = regexp.MustCompile(`\s[+«•·»]\s(.+)$|\d\.\d\d\s-\s(.+)$`)
	reRowHead      = regexp.MustCompile(`^(?:[A-Z0-9][A-Z0-9./-]{3,}|\$?\d{1,3}(?:,\d{3})*(?:\.\d\d)?|[A-Z]{1,3}|[|\[\]])\s+`)
	reSKULike      = regexp.MustCompile(`\b[A-Z]{0,4}\d{3,}[A-Z0-9./-]*\b|\b[A-Z]{2,}\d{2,}[A-Z0-9./-]*\b|\b[A-Z]\d{2,}-[A-Z0-9-]+\b`)
	reDanglingEnd  = regexp.MustCompile(`(?i)\s(to|and|with|for|of|the|a|an|in|or|your|our|that|is|are|by|on|from|at|as)$`)
	reTrailJunk    = regexp.MustCompile(`[\s\-–—:;,.|\\/_~'@#&]+$|\s[a-zA-Z]$`)
)

// bulletLine strips a bullet marker; ok is false when the line has none.
func bulletLine(s string) (string, bool) {
	m := reBulletMark.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	if st := strings.TrimSpace(s); len(st) > 1 && st[0] == '*' && st[1] != ' ' {
		return "", false // "*Teak wood is a natural product…": a footnote, not a bullet
	}
	t := strings.TrimSpace(m[1])
	if t == "" || reBulletMark.MatchString(t) && len(t) < 4 {
		return "", false
	}
	return t, true
}

// isRowLine: a SKU/price row of the table, never feature text.
func isRowLine(s string, skus map[string]bool) bool {
	for _, w := range strings.Fields(s) {
		if skus[strings.ToUpper(strings.Trim(w, ",.;:|[]()"))] {
			return true
		}
	}
	return rePriceTok.MatchString(s) && reSKULike.MatchString(s)
}

// Bullets collects feature bullets from the block lines: marked bullets ("+ Drain included",
// "« Vitreous china + Overflow") and unmarked short feature lines ("Solid brass construction").
// A continuation line (starts lower-case, is not a row) is joined to the bullet above it so
// "Overall height = 9-1/8", spout outlet to" / "deck = 6", spout reach = 9"" become one.
func Bullets(lines []string, skus map[string]bool, title string) []string {
	var out []string
	seen := map[string]bool{}
	push := func(t string, marked bool) {
		t = cleanBullet(t, marked)
		if t == "" {
			return
		}
		k := strings.ToLower(t)
		if seen[k] || title != "" && strings.EqualFold(t, title) {
			return
		}
		seen[k] = true
		out = append(out, t)
	}
	n := len(lines)
	for i := 0; i < n; i++ {
		raw := Norm(lines[i])
		t, marked := bulletLine(raw)
		if !marked {
			t = strings.TrimSpace(raw)
			if isRowLine(t, skus) {
				// a bullet printed to the right of a SKU row: "T67485-PCLHP 1416.00 + Touch-Clean nozzles"
				if m := reInlineBullet.FindStringSubmatch(t); m != nil && len(words(m[1])) >= 2 {
					t = m[1]
				} else {
					continue
				}
			} else if !featureLine(t) {
				continue
			}
		}
		// a bullet row holding several bullets: "« Vitreous china + Overflow"
		parts := reBulletSep.Split(t, -1)
		for pi, p := range parts {
			if pi == len(parts)-1 && !strings.HasSuffix(p, ".") {
				// join continuations of the last part
				for j := i + 1; j < n && j <= i+3; j++ {
					nxt := strings.TrimSpace(Norm(lines[j]))
					if nxt == "" {
						continue
					}
					if isRowLine(nxt, skus) {
						// continuation text printed after the next row's SKU and price
						if tail := rowTail(nxt, skus); tail != "" && reLowerStart.MatchString(tail) {
							nxt = tail
						} else {
							continue
						}
					}
					if _, m := bulletLine(nxt); m || !reLowerStart.MatchString(nxt) {
						break
					}
					p = p + " " + nxt
					lines[j] = "" // consumed
					if strings.HasSuffix(nxt, ".") {
						break
					}
				}
			}
			push(p, marked)
		}
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// rowTail strips the SKU, price and finish-code tokens that start a table row and returns
// the free text after them ("T67485-PNLHP 1699.00 mineral deposits" → "mineral deposits").
func rowTail(s string, skus map[string]bool) string {
	for {
		m := reRowHead.FindString(s)
		if m == "" {
			break
		}
		s = s[len(m):]
	}
	return strings.TrimSpace(s)
}

// featureLine: an unmarked line that reads as a feature ("Polished chrome plated finish").
func featureLine(t string) bool {
	ws := words(t)
	if len(ws) < 2 || len(ws) > 14 || len(t) > 110 {
		return false
	}
	if r := t[0]; !(r >= 'A' && r <= 'Z') {
		return false
	}
	if strings.ContainsAny(t, ":()") || strings.HasSuffix(t, ",") {
		return false
	}
	if !reFeatureHint.MatchString(t) || rePriceTok.MatchString(t) {
		return false
	}
	caps, upper := 0, 0
	for _, w := range ws {
		if len(w) > 1 && strings.ToUpper(w) == w && strings.ToLower(w) != w {
			upper++
		}
		if r := w[0]; r >= 'A' && r <= 'Z' {
			caps++
		}
	}
	if upper*2 > len(ws) {
		return false // a table header: "MODEL DESCRIPTION / UPGRADE OPTIONS"
	}
	if caps*5 > len(ws)*3 {
		return false // a product name or caption: "Pull-Down Faucet with Square Spout"
	}
	return junkRatio(t) < 0.2
}

// cleanBullet tidies one bullet; "" means drop it.
func cleanBullet(t string, marked bool) string {
	t = strings.TrimSpace(reTrailJunk.ReplaceAllString(strings.TrimSpace(t), ""))
	t = strings.TrimLeft(t, "-–—+«»•·*=> ")
	if t == "" {
		return ""
	}
	ws := words(t)
	if len(ws) < 2 || !marked && len(ws) < 3 || len(ws) > 22 || len(t) < 8 {
		return ""
	}
	if reBulletJunk.MatchString(t) || rePriceTok.MatchString(t) || junkRatio(t) > 0.15 || reImperative.MatchString(t) {
		return ""
	}
	if reNonASCII.MatchString(t) || strings.ContainsAny(t, "@|_~{}[]") {
		return "" // translations, OCR debris
	}
	for _, m := range reOddFrac.FindAllStringSubmatch(t, -1) {
		if d, _ := strconv.Atoi(m[1]); !okDen[d] {
			return "" // "13 21/34"": a fraction the OCR could not read
		}
	}
	if reDanglingEnd.MatchString(t) {
		return ""
	}
	if r := t[0]; !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
		return ""
	}
	t = sentenceCase(t)
	t = strings.TrimSuffix(t, ".")
	return tidyDims(t)
}

// tidyDims rewrites every printed length in spec-sheet form ("91/8"" → "9-1/8"").
func tidyDims(s string) string {
	hits := findDims(s)
	if len(hits) == 0 {
		return s
	}
	var b strings.Builder
	pos := 0
	for _, h := range hits {
		b.WriteString(s[pos:h.Start])
		b.WriteString(h.String())
		pos = h.End
	}
	b.WriteString(s[pos:])
	return b.String()
}

// Description assembles a short prose description from unmarked paragraph lines: two or
// more consecutive lines of plain words, or one long sentence. It never cuts mid-word and
// never carries prices, page references or SKUs.
func Description(lines []string, skus map[string]bool) string {
	var para []string
	var best string
	flush := func() {
		if len(para) == 0 {
			return
		}
		t := Flat(para)
		para = nil
		if len(words(t)) < 15 || reBulletJunk.MatchString(t) || rePronoun.MatchString(t) || rePriceTok.MatchString(t) || reSKULike.MatchString(t) || junkRatio(t) > 0.1 {
			return // a marketing letter ("We're excited to share…") is not a product description
		}
		if reImperative.MatchString(t) || reNonASCII.MatchString(t) || !reDescWord.MatchString(t) {
			return // instructions, translations, text about something other than the product
		}
		if len(t) > 420 {
			cut := strings.LastIndex(t[:420], ". ")
			if cut < 120 {
				return
			}
			t = t[:cut+1]
		}
		if reDanglingEnd.MatchString(strings.TrimRight(t, ".")) {
			return
		}
		if len(t) > len(best) {
			best = t
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(Norm(l))
		if _, marked := bulletLine(t); marked || t == "" || isRowLine(t, skus) || !proseLine(t) {
			flush()
			continue
		}
		para = append(para, t)
	}
	flush()
	return best
}

var reProseWord = regexp.MustCompile(`(?i)\b(the|and|with|for|of|your|our|is|are|its|this|that|from|to|in|a|an)\b`)

// proseLine: mostly lower-case words with function words in them, no table markers.
func proseLine(t string) bool {
	ws := words(t)
	if len(ws) < 5 || strings.Contains(t, "|") || strings.ContainsAny(t, "$@") {
		return false
	}
	if strings.ToUpper(t) == t {
		return false
	}
	lower := 0
	for _, w := range ws {
		if r := w[0]; r >= 'a' && r <= 'z' {
			lower++
		}
	}
	return lower*3 >= len(ws)*2 && reProseWord.MatchString(t) && junkRatio(t) < 0.15
}
