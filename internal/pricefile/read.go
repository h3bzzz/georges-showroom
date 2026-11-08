package pricefile

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Row is one product line of the feed, already mapped to our fields.
type Row struct {
	Brand        string
	SKU          string
	UPC          string
	Description  string
	Finish       string
	ListCents    int64 // manufacturer list / MSRP
	CostCents    int64 // what the company pays
	PriceCents   int64 // the site's selling price, when the feed carries one
	LeadDays     int
	Discontinued bool
	Extra        map[string]string // every other column, for inspection
}

// Columns maps our fields to header names found in the sheet.
type Columns struct {
	Brand, SKU, UPC, Description, Finish, List, Cost, Price, Lead, Status string
	Headers                                                               []string
}

// field name patterns tried in order; the first header matching wins
var colPatterns = map[string][]*regexp.Regexp{
	"brand":       rx(`^brand( ?name)?$`, `^manufacturer`, `^mfg(\.| )?(name)?$`, `^vendor( ?name)?$`, `brand`),
	"sku":         rx(`^sku$`, `^item ?(number|no|#|code)?$`, `^model ?(number|no|#)?$`, `^part ?(number|no|#)?$`, `^product ?(number|no|#|code|id)$`, `^mfg(\.| )?(part|item|number|#)`, `sku`, `model`),
	"upc":         rx(`^upc`, `^gtin`, `^ean`, `upc`),
	"description": rx(`^(short |long |product |item )?desc`, `^(product |item )?name$`, `^title$`, `desc`),
	"finish":      rx(`^finish`, `^colou?r`, `finish`),
	"list":        rx(`^list( price)?$`, `^msrp`, `^retail( price)?$`, `^(mfg|manufacturer) list`, `list`, `msrp`, `retail`),
	"cost":        rx(`^cost$`, `^(your |company |net |dealer |wholesale |unit )?cost`, `^net( price)?$`, `^dealer( price)?$`, `^wholesale`, `cost`, `\bnet\b`),
	"price":       rx(`^(sell|selling|site|web|display|customer|our) ?price`, `^price$`, `^sale price`, `sell`),
	"lead":        rx(`lead ?time`, `^lead`, `ship(s|ping)? (in|days|time)`),
	"status":      rx(`^status`, `discontinu`, `^active`, `^available`),
}

func rx(ps ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ps))
	for i, p := range ps {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}

// Detect maps headers to fields. Exact and anchored patterns are tried for every field before
// loose ones, so "List Price" is not taken by the loose `price` of the selling-price field.
func Detect(headers []string) Columns {
	c := Columns{Headers: headers}
	norm := make([]string, len(headers))
	for i, h := range headers {
		norm[i] = strings.TrimSpace(strings.ToLower(h))
	}
	used := map[int]bool{}
	pick := func(field string) string {
		pats := colPatterns[field]
		for _, p := range pats {
			for i, h := range norm {
				if h != "" && !used[i] && p.MatchString(h) {
					used[i] = true
					return headers[i]
				}
			}
		}
		return ""
	}
	// the most specific fields first so loose patterns cannot steal their columns
	c.SKU = pick("sku")
	c.Brand = pick("brand")
	c.UPC = pick("upc")
	c.List = pick("list")
	c.Cost = pick("cost")
	c.Price = pick("price")
	c.Description = pick("description")
	c.Finish = pick("finish")
	c.Lead = pick("lead")
	c.Status = pick("status")
	return c
}

// Complete reports whether the sheet can be ingested (a SKU and at least one price).
func (c Columns) Complete() bool {
	return c.SKU != "" && (c.List != "" || c.Cost != "" || c.Price != "")
}

// Source is one sheet or CSV inside the download.
type Source struct {
	Name    string
	Headers []string
	Cols    Columns
	rows    func(func(Row) bool) error
}

// Rows calls fn for every mapped row until fn returns false.
func (s *Source) Rows(fn func(Row) bool) error { return s.rows(fn) }

// Open reads a downloaded price file: a zip of xlsx/csv files, or one such file.
func Open(path string) ([]*Source, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		return openZip(path)
	case ".xlsx", ".xlsm":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return openXLSX(filepath.Base(path), f)
	case ".csv", ".txt":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		s, err := openCSV(filepath.Base(path), f)
		if err != nil {
			return nil, err
		}
		return []*Source{s}, nil
	}
	return nil, fmt.Errorf("pricefile: unknown file type %s", path)
}

func openZip(path string) ([]*Source, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("pricefile: %w", err)
	}
	defer zr.Close()
	var out []*Source
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(filepath.Ext(f.Name)) {
		case ".xlsx", ".xlsm":
			ss, err := openXLSX(f.Name, rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			out = append(out, ss...)
		case ".csv", ".txt":
			s, err := openCSV(f.Name, rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			out = append(out, s)
		default:
			rc.Close()
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pricefile: %s holds no xlsx or csv", path)
	}
	return out, nil
}

// openXLSX loads a workbook (buffered: excelize needs a seekable source) and exposes every
// sheet that has a header row. Rows stream from the sheet XML, so a million-row sheet is fine.
func openXLSX(name string, r io.Reader) ([]*Source, error) {
	x, err := excelize.OpenReader(r, excelize.Options{UnzipXMLSizeLimit: 32 << 20})
	if err != nil {
		return nil, err
	}
	var out []*Source
	for _, sheet := range x.GetSheetList() {
		rows, err := x.Rows(sheet)
		if err != nil {
			return nil, err
		}
		if !rows.Next() {
			rows.Close()
			continue
		}
		headers, _ := rows.Columns()
		rows.Close()
		headers = trimAll(headers)
		if countNonEmpty(headers) < 2 {
			continue
		}
		sheet := sheet
		src := &Source{Name: name + "!" + sheet, Headers: headers, Cols: Detect(headers)}
		src.rows = func(fn func(Row) bool) error {
			rows, err := x.Rows(sheet)
			if err != nil {
				return err
			}
			defer rows.Close()
			first := true
			for rows.Next() {
				cells, err := rows.Columns()
				if err != nil {
					return err
				}
				if first {
					first = false
					continue
				}
				if row, ok := mapRow(src.Cols, headers, cells); ok && !fn(row) {
					return nil
				}
			}
			return rows.Error()
		}
		out = append(out, src)
	}
	return out, nil
}

func openCSV(name string, r io.Reader) (*Source, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	comma := ','
	if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, "\t") > strings.Count(first, ",") {
		comma = '\t'
	} else if strings.Count(first, "|") > strings.Count(first, ",") {
		comma = '|'
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = comma
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	headers, err := cr.Read()
	if err != nil {
		return nil, err
	}
	headers = trimAll(headers)
	src := &Source{Name: name, Headers: headers, Cols: Detect(headers)}
	src.rows = func(fn func(Row) bool) error {
		cr := csv.NewReader(strings.NewReader(text))
		cr.Comma = comma
		cr.FieldsPerRecord = -1
		cr.LazyQuotes = true
		if _, err := cr.Read(); err != nil {
			return err
		}
		for {
			cells, err := cr.Read()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if row, ok := mapRow(src.Cols, headers, cells); ok && !fn(row) {
				return nil
			}
		}
	}
	return src, nil
}

func trimAll(s []string) []string {
	for i := range s {
		s[i] = strings.TrimSpace(s[i])
	}
	return s
}

func countNonEmpty(s []string) int {
	n := 0
	for _, x := range s {
		if x != "" {
			n++
		}
	}
	return n
}

// mapRow builds a Row from cells by header name.
func mapRow(c Columns, headers, cells []string) (Row, bool) {
	get := func(h string) string {
		if h == "" {
			return ""
		}
		for i, x := range headers {
			if x == h && i < len(cells) {
				return strings.TrimSpace(cells[i])
			}
		}
		return ""
	}
	r := Row{
		Brand: get(c.Brand), SKU: get(c.SKU), UPC: get(c.UPC), Description: get(c.Description), Finish: get(c.Finish),
		ListCents: Cents(get(c.List)), CostCents: Cents(get(c.Cost)), PriceCents: Cents(get(c.Price)),
		LeadDays: leadDays(get(c.Lead)), Extra: map[string]string{},
	}
	if st := strings.ToLower(get(c.Status)); st != "" {
		r.Discontinued = strings.Contains(st, "discontinu") || st == "inactive" || st == "n" || st == "no" || st == "false" || st == "0"
	}
	if r.SKU == "" {
		return r, false
	}
	mapped := map[string]bool{c.Brand: true, c.SKU: true, c.UPC: true, c.Description: true, c.Finish: true, c.List: true, c.Cost: true, c.Price: true, c.Lead: true, c.Status: true}
	for i, h := range headers {
		if h != "" && !mapped[h] && i < len(cells) && strings.TrimSpace(cells[i]) != "" {
			r.Extra[h] = strings.TrimSpace(cells[i])
		}
	}
	return r, true
}

var reMoney = regexp.MustCompile(`-?\d[\d,]*(\.\d+)?`)

// Cents parses a money cell ("$1,234.50", "1234.5", "1,234") to cents; 0 when absent or not a
// number. Excel stores numbers as plain decimals, so "1234.5" is the common case.
func Cents(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	m := reMoney.FindString(s)
	if m == "" {
		return 0
	}
	m = strings.ReplaceAll(m, ",", "")
	f, err := strconv.ParseFloat(m, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f*100 + 0.5)
}

var reDays = regexp.MustCompile(`\d+`)

func leadDays(s string) int {
	if s == "" {
		return 0
	}
	m := reDays.FindAllString(s, -1)
	if len(m) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(m[len(m)-1]) // "2-3 weeks" → 3
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "week"):
		n *= 7
	case strings.Contains(l, "month"):
		n *= 30
	}
	return n
}
