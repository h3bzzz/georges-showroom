package store

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Measurements printed in catalogs become numbers a query can match: `32-1/4" x 18-7/8"`,
// `8"`, `5/8"`, `9.5"`, `96 mm`, `60 cm`, `10''`. family_dims holds one row per (family, key,
// inches) so "66 inch tub" or "8 inch centers" retrieve by range (migration 00046).

// DimKeys are the attribute keys that carry a size, with the word a shopper uses for each.
var DimKeys = []string{"Width", "Length", "Height", "Depth", "Diameter", "Centers", "Spout Reach", "Spout Height",
	"Overall Height", "Nominal Size", "Dimensions", "Projection", "Bowl Depth", "Basin Depth", "Hose Length", "Arm Length",
	"Max Height", "Minimum Cabinet", "Door Thickness", "Backset", "Knob Diameter", "Rose Diameter", "Basin Interior"}

// dimHints map a query word to the keys it narrows a measurement to.
var dimHints = map[string][]string{
	"wide": {"Width", "Nominal Size", "Dimensions"}, "width": {"Width", "Nominal Size", "Dimensions"}, "w": {"Width"},
	"long": {"Length", "Dimensions"}, "length": {"Length", "Dimensions"}, "l": {"Length"},
	"tall": {"Height", "Overall Height", "Spout Height"}, "high": {"Height", "Overall Height", "Spout Height"}, "height": {"Height", "Overall Height", "Spout Height"}, "h": {"Height"},
	"deep": {"Depth", "Bowl Depth", "Basin Depth", "Dimensions"}, "depth": {"Depth", "Bowl Depth", "Basin Depth", "Dimensions"}, "d": {"Depth"},
	"centers": {"Centers"}, "center": {"Centers"}, "centre": {"Centers"}, "centres": {"Centers"}, "cc": {"Centers"}, "c-c": {"Centers"}, "spread": {"Centers"}, "oc": {"Centers"},
	"reach": {"Spout Reach"}, "projection": {"Projection", "Spout Reach"},
	"diameter": {"Diameter", "Knob Diameter", "Rose Diameter"}, "round": {"Diameter", "Knob Diameter", "Rose Diameter"}, "dia": {"Diameter", "Knob Diameter", "Rose Diameter"},
	"backset": {"Backset"}, "hose": {"Hose Length"}, "cabinet": {"Minimum Cabinet"},
}

var (
	reInchPart = regexp.MustCompile(`(\d+)/(\d+)|(\d+(?:\.\d+)?)(?:[-\s]+(\d+)/(\d+))?`)
	reMetric   = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(mm|cm|m)\b`)
)

// ParseInches returns every length in s, in inches. "32-1/4" x 18-7/8"" → 32.25, 18.875;
// "96 mm" → 3.78; a bare number counts as inches (price books omit the mark in tables).
func ParseInches(s string) []float64 {
	s = strings.ReplaceAll(s, "''", `"`)
	s = strings.ReplaceAll(s, "”", `"`)
	var out []float64
	if ms := reMetric.FindAllStringSubmatch(s, -1); len(ms) > 0 {
		for _, m := range ms {
			v, _ := strconv.ParseFloat(m[1], 64)
			switch strings.ToLower(m[2]) {
			case "mm":
				v /= 25.4
			case "cm":
				v /= 2.54
			case "m":
				v *= 39.37
			}
			out = append(out, round2(v))
		}
		return out
	}
	for _, m := range reInchPart.FindAllStringSubmatch(s, -1) {
		if m[1] != "" { // bare fraction 5/8
			n, _ := strconv.ParseFloat(m[1], 64)
			d, _ := strconv.ParseFloat(m[2], 64)
			if d > 0 {
				out = append(out, round2(n/d))
			}
			continue
		}
		v, _ := strconv.ParseFloat(m[3], 64)
		if m[4] != "" {
			n, _ := strconv.ParseFloat(m[4], 64)
			d, _ := strconv.ParseFloat(m[5], 64)
			if d > 0 && n < d {
				v += n / d
			}
		}
		if v > 0 && v < 1000 {
			out = append(out, round2(v))
		}
	}
	return out
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

// RefreshDims rebuilds family_dims from the size attributes of every family.
func (s *Store) RefreshDims(ctx context.Context) (int64, error) {
	rows, err := s.Pool.Query(ctx, `SELECT f.id, e.key, e.value FROM product_families f, jsonb_each_text(f.attributes) e WHERE e.key = ANY($1)`, DimKeys)
	if err != nil {
		return 0, err
	}
	type dim struct {
		id  int64
		key string
		in  float64
	}
	var dims []dim
	for rows.Next() {
		var id int64
		var k, v string
		if err := rows.Scan(&id, &k, &v); err != nil {
			rows.Close()
			return 0, err
		}
		seen := map[float64]bool{}
		for _, in := range ParseInches(v) {
			if in <= 0 || in > 400 || seen[in] {
				continue
			}
			seen[in] = true
			dims = append(dims, dim{id, k, in})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `TRUNCATE family_dims`); err != nil {
		return 0, err
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"family_dims"}, []string{"family_id", "key", "inches"}, pgx.CopyFromSlice(len(dims), func(i int) ([]any, error) {
		return []any{dims[i].id, dims[i].key, float32(dims[i].in)}, nil
	}))
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// reInchMark turns 66" / 66” / 66″ into 66in before quotes are stripped from the query.
var reInchMark = regexp.MustCompile(`(\d)\s*(?:"|''|″|”)`)

var reDimToken = regexp.MustCompile(`^(\d+(?:\.\d+)?)(?:-(\d+)/(\d+))?(in|inch|inches|mm|cm)?$`)
var reFraction = regexp.MustCompile(`^(\d+)/(\d+)$`)
var reDimPair = regexp.MustCompile(`^(\d+(?:\.\d+)?)x(\d+(?:\.\d+)?)(in|inch|inches)?$`)

var dimUnits = map[string]bool{"in": true, "inch": true, "inches": true, "mm": true, "cm": true}

// dimRun reads a measurement starting at words[i] and returns the index after it with the
// concept, or i when there is none. Forms: `66in` (from 66"), `66 inch`, `5-1/2 inch`,
// `5 1/2 in`, `120 mm`, `60x32`, and a number followed by a hint word (`30 wide`, `8 centers`).
// A bare number is a size only from 12 up (a tub, a vanity, a mirror): "2 handle faucet"
// and "3 hole" stay counts.
func dimRun(words []string, i int) (int, Concept) {
	w := strings.Trim(words[i], "-–—.:;()")
	if m := reDimPair.FindStringSubmatch(w); m != nil {
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[2], 64)
		if a >= 3 && b >= 3 {
			// two sizes: keep the first as the concept; the second rides along in the label only
			return i + 1, Concept{Kind: "dim", Label: fmtInch(a) + " x " + fmtInch(b), Inches: a}
		}
	}
	m := reDimToken.FindStringSubmatch(w)
	if m == nil {
		return i, Concept{}
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	if m[2] != "" {
		n, _ := strconv.ParseFloat(m[2], 64)
		d, _ := strconv.ParseFloat(m[3], 64)
		if d > 0 && n < d {
			v += n / d
		}
	}
	unit := m[4]
	j := i + 1
	if unit == "" && j < len(words) {
		if f := reFraction.FindStringSubmatch(words[j]); f != nil {
			n, _ := strconv.ParseFloat(f[1], 64)
			d, _ := strconv.ParseFloat(f[2], 64)
			if d > 0 && n < d {
				v += n / d
				j++
			}
		}
	}
	if unit == "" && j < len(words) && dimUnits[strings.ToLower(words[j])] {
		unit = strings.ToLower(words[j])
		j++
	}
	switch unit {
	case "mm":
		v = round2(v / 25.4)
	case "cm":
		v = round2(v / 2.54)
	}
	var keys []string
	if j < len(words) {
		if k, ok := dimHints[strings.ToLower(strings.Trim(words[j], "-–—.:;()\""))]; ok {
			keys = k
			j++
		}
	}
	if v <= 0 || v > 200 {
		return i, Concept{}
	}
	if unit == "" && keys == nil && (v < 12 || v != float64(int(v))) {
		return i, Concept{}
	}
	label := fmtInch(v)
	if keys != nil {
		label += " " + strings.ToLower(strings.Trim(words[j-1], "-–—.:;()\""))
	}
	return j, Concept{Kind: "dim", Label: label, Inches: round2(v), DimKeys: keys}
}

func fmtInch(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + `"`
}

func (c Concept) dimKeys() []string {
	if len(c.DimKeys) > 0 {
		return c.DimKeys
	}
	return DimKeys
}

// dimTol is how far a printed size may sit from the one asked: a 66" tub is often 65-3/4".
func (c Concept) dimTol() float64 {
	switch {
	case c.Inches < 12:
		return 0.26
	case c.Inches < 36:
		return 0.6
	}
	return 1.0
}
