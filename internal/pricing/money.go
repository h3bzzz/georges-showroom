// Package pricing is the one place that decides what a price is: how a printed money token is
// parsed, which values are plausible, and how a brand's prices are fenced for outliers.
//
// Prices move through three sources with a fixed precedence:
//
//	site    – the price George's site itself shows (authoritative sell price)
//	feed    – the daily Unilog price file over FTP (list + cost per SKU; not yet wired)
//	catalog – a list price read by OCR from a vendor price book, sold at the brand multiplier
//
// OCR is the only source that can invent a number, so every catalog price must pass ParseCents
// and then the brand-level Fence before it is shown.
package pricing

import (
	"regexp"
	"strconv"
	"strings"
)

// money token: optional $, either comma-grouped thousands or a plain run of digits, optional cents
var reMoney = regexp.MustCompile(`^\$?\s?(\d{1,3}(?:,\d{3})+|\d{1,6})(?:\.(\d{2}))?$`)

// Bounds for a single printed list price. Below $10 a decimal is a flow rate ("6.29 gpm") or a
// weight far more often than a price, and the most expensive single SKU in any book (a
// freestanding tub, a range) is well under $75,000.
const (
	MinCents int64 = 1000
	MaxCents int64 = 7_500_000
)

// ParseCents reads a printed price such as "$1,787.00", "2,681.00", "327.00" or "$450".
//
// It is deliberately strict because the same page carries page numbers, quantities, model
// numbers and index references that look like money: "186,193" (pages 186 and 193 in a
// numerical index), "245" (a page), "20099" (a Gessi SKU). A token counts as money only when
// it carries a dollar sign or a two-digit cents part; a bare thousands-comma is not enough.
func ParseCents(tok string) (int64, bool) {
	t := strings.TrimSpace(tok)
	t = strings.Trim(t, "*†‡,;:")
	hasDollar := strings.HasPrefix(t, "$")
	m := reMoney.FindStringSubmatch(t)
	if m == nil {
		return 0, false
	}
	if !hasDollar && m[2] == "" {
		return 0, false // "1,234" / "245": page number, quantity or index reference
	}
	d, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	if err != nil {
		return 0, false
	}
	var c int64
	if m[2] != "" {
		c, _ = strconv.ParseInt(m[2], 10, 64)
	}
	v := d*100 + c
	if v < MinCents || v > MaxCents {
		return 0, false
	}
	return v, true
}

// LooksLikeMoney reports whether a token would parse, ignoring the value bounds. Used to tell a
// price column from a page-number column.
func LooksLikeMoney(tok string) bool {
	t := strings.Trim(strings.TrimSpace(tok), "*†‡,;:")
	m := reMoney.FindStringSubmatch(t)
	return m != nil && (strings.HasPrefix(t, "$") || m[2] != "")
}

// ParseBareDollars reads a whole-dollar price printed without any sign or cents ("1095",
// "511"), the style of the California Faucets price book. Because such a token is
// indistinguishable from a page number or quantity on its own, callers must only use it for
// the last token of a line that also carries a product description and no index layout.
func ParseBareDollars(tok string) (int64, bool) {
	t := strings.Trim(strings.TrimSpace(tok), "*†‡,;:")
	if len(t) < 2 || len(t) > 5 {
		return 0, false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	d, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return 0, false
	}
	v := d * 100
	if v < MinCents || v > MaxCents {
		return 0, false
	}
	return v, true
}
