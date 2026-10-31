// Package site parses georgesshowroom.com (Unilog CX1 "B41" template) pages into Go structs.
package site

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const BaseURL = "https://www.georgesshowroom.com"

var (
	reMoney   = regexp.MustCompile(`\$\s*([\d,]+(?:\.\d{1,2})?)`)
	rePageOf  = regexp.MustCompile(`Page\s+(\d+)\s+of\s+(\d+)`)
	reCount   = regexp.MustCompile(`^(.*?)\s*\((\d+)\)\s*$`)
	reSpace   = regexp.MustCompile(`\s+`)
	reVerQS   = regexp.MustCompile(`\?ver=[^&"']*`)
	rePctSave = regexp.MustCompile(`Save\s+(\d+)%`)
)

func clean(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

// money parses the first "$1,234.56" in s into cents; ok=false when none.
func money(s string) (cents int64, ok bool) {
	m := reMoney.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	return int64(f*100 + 0.5), true
}

// moneyAll returns every dollar amount in s, in order.
func moneyAll(s string) []int64 {
	var out []int64
	for _, m := range reMoney.FindAllStringSubmatch(s, -1) {
		f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err == nil {
			out = append(out, int64(f*100+0.5))
		}
	}
	return out
}

// abs makes a site-relative href absolute and strips the image cache-buster.
func abs(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") {
		return ""
	}
	href = reVerQS.ReplaceAllString(href, "")
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "/") {
		return BaseURL + href
	}
	return href
}

func attrOr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.TrimSpace(v)
}

// labeledCount splits "Chrome (1419)" into ("Chrome", 1419).
func labeledCount(s string) (string, int) {
	s = clean(s)
	if m := reCount.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		return strings.TrimSpace(m[1]), n
	}
	return s, 0
}
