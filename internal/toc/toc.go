// Package toc derives a table of contents for a catalog from OCR word boxes: big, short text
// near the top of a page is a section heading; consecutive pages under the same heading fold
// into one entry.
package toc

import (
	"regexp"
	"sort"
	"strings"

	"georges_/internal/ocr"
)

type Entry struct {
	Page  int
	Title string
	Level int
}

var (
	reDigits = regexp.MustCompile(`\d`)
	reJunk   = regexp.MustCompile(`[^A-Za-z0-9&'’\-/ ]`)
)

// PageHeading returns the most likely heading on one page, or "".
func PageHeading(words []ocr.Word, pageH int) string {
	if len(words) < 3 {
		return ""
	}
	hs := make([]int, 0, len(words))
	for _, w := range words {
		if w.Conf >= 60 && w.H > 4 {
			hs = append(hs, w.H)
		}
	}
	if len(hs) < 3 {
		return ""
	}
	sort.Ints(hs)
	median := hs[len(hs)/2]
	minH := max(int(float64(median)*1.9), 22)

	type key struct{ b, p, l int }
	lines := map[key][]ocr.Word{}
	for _, w := range words {
		if w.Conf < 60 || w.H < minH {
			continue
		}
		k := key{w.Block, w.Par, w.Line}
		lines[k] = append(lines[k], w)
	}
	best, bestScore := "", 0.0
	for _, ws := range lines {
		sort.Slice(ws, func(i, j int) bool { return ws[i].X < ws[j].X })
		var parts []string
		avgH, y := 0, ws[0].Y
		for _, w := range ws {
			parts = append(parts, w.Text)
			avgH += w.H
		}
		avgH /= len(ws)
		t := strings.TrimSpace(reJunk.ReplaceAllString(strings.Join(parts, " "), " "))
		t = strings.Join(strings.Fields(t), " ")
		letters := 0
		for _, r := range t {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				letters++
			}
		}
		if letters < 4 || len(t) > 48 || len(t) < 3 || ocr.LooksLikeSKU(t) {
			continue
		}
		short, bad := 0, false
		for _, w := range strings.Fields(t) {
			if len(w) == 1 {
				short++
			}
			if mixedDigits(w) || tripleRun(w) || strings.EqualFold(w, "com") || strings.EqualFold(w, "www") {
				bad = true
			}
		}
		if short > 0 || bad || len(strings.Fields(t)) > 7 {
			continue // "G Ssi", "5lack", "llllf", "gessi com", long sentences
		}
		if float64(len(reDigits.FindAllString(t, -1))) > float64(len(t))*0.3 {
			continue // price columns, dimensions
		}
		// score: bigger and higher on the page wins
		score := float64(avgH) * (1.0 - 0.5*float64(y)/float64(max(pageH, 1)))
		if score > bestScore {
			bestScore, best = score, t
		}
	}
	return titleCase(best)
}

// mixedDigits flags OCR confusions like "5lack" or "Finishesl2".
func mixedDigits(w string) bool {
	d, l := 0, 0
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			d++
		case r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			l++
		}
	}
	return d > 0 && l > 0
}

// tripleRun flags "llllf"-style garbage.
func tripleRun(w string) bool {
	lw := strings.ToLower(w)
	for i := 2; i < len(lw); i++ {
		if lw[i] == lw[i-1] && lw[i] == lw[i-2] {
			return true
		}
	}
	return false
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	up := strings.ToUpper(s) == s
	if !up {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Fold turns per-page headings into entries, merging runs of the same heading and dropping
// running headers that repeat across much of the book.
func Fold(headings map[int]string, pages int) []Entry {
	freq := map[string]int{}
	for _, h := range headings {
		if h != "" {
			freq[strings.ToLower(h)]++
		}
	}
	var out []Entry
	seen := map[string]bool{}
	for p := 1; p <= pages; p++ {
		h := headings[p]
		if h == "" || seen[strings.ToLower(h)] {
			continue // first occurrence of a section wins; later repeats are running headers
		}
		if pages >= 20 && freq[strings.ToLower(h)] > pages/4 {
			continue
		}
		seen[strings.ToLower(h)] = true
		out = append(out, Entry{Page: p, Title: h, Level: 1})
	}
	return out
}
