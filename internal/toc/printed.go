package toc

import (
	"regexp"
	"strconv"
	"strings"
)

// Printed is an entry parsed from a book's own "Table of Contents" page.
type Printed struct {
	Title   string
	Group   string // e.g. Kitchen, Bath
	Printed int    // page number as printed in the book
	Level   int    // 1 group, 2 entry
}

var (
	reContentsHead = regexp.MustCompile(`(?i)^\s*(table of )?contents\s*$`)
	rePageRef      = regexp.MustCompile(`(?i)(?:^|\s)(?:p\.?\s*|page\s+)?(\d{1,4})\s*$`)
	reOnlyPage     = regexp.MustCompile(`(?i)^\s*(?:p\.?\s*|page\s+)(\d{1,4})\s*$`)
	reGroup        = regexp.MustCompile(`^(?i)([A-Za-z][A-Za-z &/-]{2,30}?)(\s*\(cont\.?\))?\s*$`)
	reFooterNum    = regexp.MustCompile(`^\s*\d{1,4}\s*$`)
)

// IsContentsPage reports whether an OCR page text looks like a printed table of contents.
func IsContentsPage(text string) bool {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if i > 6 {
			break
		}
		if reContentsHead.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

// ParsePrinted extracts entries from a contents page. Lines like "Davoli | K10 Series"
// followed by "p.38" (or ending in a page number) become entries; short un-numbered lines
// such as "Kitchen" / "Bath (cont.)" become the current group.
func ParsePrinted(text string) []Printed {
	var out []Printed
	group := ""
	var pending []string // titles waiting for a page number
	lines := strings.Split(text, "\n")
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if l == "" || reContentsHead.MatchString(l) {
			continue
		}
		if m := reOnlyPage.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[1])
			if len(pending) > 0 {
				out = append(out, Printed{Title: clean(pending[len(pending)-1]), Group: group, Printed: n, Level: 2})
				pending = pending[:len(pending)-1]
			}
			continue
		}
		if m := rePageRef.FindStringSubmatch(l); m != nil && !reFooterNum.MatchString(l) {
			n, _ := strconv.Atoi(m[1])
			title := strings.TrimSpace(strings.TrimSuffix(l, m[0]))
			title = strings.TrimRight(title, " .|")
			if letters(title) >= 3 && n > 0 && n < 5000 {
				out = append(out, Printed{Title: clean(title), Group: group, Printed: n, Level: 2})
				continue
			}
		}
		if m := reGroup.FindStringSubmatch(l); m != nil && letters(l) <= 30 && len(strings.Fields(l)) <= 3 && !strings.Contains(l, "|") && !strings.ContainsAny(l, "0123456789") {
			g := strings.TrimSpace(m[1])
			// a group is a short standalone word like Kitchen / Bath / Hardware / Accessories
			if isGroupWord(g) {
				group = strings.Title(strings.ToLower(g))
				pending = nil
				continue
			}
		}
		if letters(l) >= 3 && !strings.ContainsAny(l, "£$=") {
			pending = append(pending, l)
			if len(pending) > 3 {
				pending = pending[1:]
			}
		}
	}
	return out
}

func isGroupWord(g string) bool {
	g = strings.ToLower(g)
	for _, w := range []string{"kitchen", "bath", "bathroom", "hardware", "accessories", "shower", "showers", "tub", "faucets", "sinks", "toilets", "lighting", "appliances", "outdoor", "commercial", "parts", "components", "drains", "finishes", "cabinet", "door", "vanities", "mirrors"} {
		if strings.Contains(g, w) {
			return true
		}
	}
	return false
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "|", " · ")
	return strings.Join(strings.Fields(s), " ")
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			n++
		}
	}
	return n
}

// Offset estimates (book page index − printed page number) by matching printed entries to
// detected headings within a small window. Returns the most common offset and its votes.
func Offset(entries []Printed, headings map[int]string, pages int) (int, int) {
	votes := map[int]int{}
	for _, e := range entries {
		first := strings.ToLower(strings.Fields(strings.ReplaceAll(e.Title, "·", " "))[0])
		if len(first) < 3 {
			continue
		}
		for d := -2; d <= 8; d++ {
			p := e.Printed + d
			if p < 1 || p > pages {
				continue
			}
			if strings.Contains(strings.ToLower(headings[p]), first) {
				votes[d]++
			}
		}
	}
	best, n := 0, 0
	for d, v := range votes {
		if v > n {
			best, n = d, v
		}
	}
	return best, n
}
