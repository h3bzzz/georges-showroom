// Package feed reads the Unilog data feed: per-brand XML files on the company FTP account
// (data/<BRAND>-<date>.xml with the product records, data/<BRAND>-EXT-<date>.xml with extra
// attributes) and loads them into feed_products, the raw store families and items are derived
// from for brands the catalog pipeline never covered.
package feed

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Record is one <Product> element as a flat map of child element → text.
type Record map[string]string

// Parse streams every <Product> of a feed file to fn. Unknown child elements are kept, so new
// Unilog fields never need a code change to be stored.
func Parse(r io.Reader, fn func(Record) error) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	var cur Record
	var field string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("feed: xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case t.Name.Local == "Product":
				cur = Record{}
			case cur != nil:
				field = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if cur != nil && field != "" {
				text.Write(t)
			}
		case xml.EndElement:
			switch {
			case t.Name.Local == "Product":
				if cur != nil {
					if err := fn(cur); err != nil {
						return err
					}
				}
				cur = nil
			case cur != nil && t.Name.Local == field:
				if v := strings.TrimSpace(text.String()); v != "" && v != "None" {
					cur[field] = v
				}
				field = ""
			}
		}
	}
}

// ParseFile opens and parses one file.
func ParseFile(path string, fn func(Record) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Parse(f, fn)
}

var reSKUNorm = regexp.MustCompile(`[^A-Z0-9]`)

// NormSKU is the join key shared with items.sku_norm.
func NormSKU(s string) string { return reSKUNorm.ReplaceAllString(strings.ToUpper(s), "") }

// Cents parses a feed decimal ("244.9000") to cents; 0 when empty or not a number.
func Cents(s string) int64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f*100 + 0.5)
}

// columns are the record fields stored in their own column; everything else goes to attrs.
var columns = map[string]bool{
	"productID": true, "brandName": true, "brandCode": true, "sku": true, "altSku": true, "productIndex": true, "model": true, "upc": true,
	"cx1CategoryID": true, "type": true, "group": true, "category": true, "subCategory": true, "mCategory": true, "mSubCategory": true,
	"collection": true, "productName": true, "finish": true, "finishGroup": true, "msrp": true, "mapPrice": true, "dnPrice": true,
	"priceGroup": true, "currency": true, "imageName": true, "imagePath": true, "imageType": true, "avSpecLink": true, "avInstallLink": true,
	"avWebLink": true, "atDescription": true, "atMarketingCopy": true, "createDate": true, "updateDate": true, "country": true,
}

var reAltImage = regexp.MustCompile(`^avAltImagePath(\d+)$`)
var reBullet = regexp.MustCompile(`^atBullet(\d+)$`)

// Bullets returns atBullet1..n in order.
func (r Record) Bullets() []string {
	type nb struct {
		n int
		s string
	}
	var out []nb
	for k, v := range r {
		if m := reBullet.FindStringSubmatch(k); m != nil {
			n, _ := strconv.Atoi(m[1])
			out = append(out, nb{n, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	res := make([]string, 0, len(out))
	for _, b := range out {
		res = append(res, b.s)
	}
	return res
}

// AltImages returns avAltImagePath1..n in order.
func (r Record) AltImages() []string {
	type nb struct {
		n int
		s string
	}
	var out []nb
	for k, v := range r {
		if m := reAltImage.FindStringSubmatch(k); m != nil {
			n, _ := strconv.Atoi(m[1])
			out = append(out, nb{n, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	res := make([]string, 0, len(out))
	for _, b := range out {
		res = append(res, b.s)
	}
	return res
}

// Attrs returns the attribute fields (av*, XV_*, XL_*, atBullet* excluded) with readable keys.
func (r Record) Attrs() map[string]string {
	out := map[string]string{}
	for k, v := range r {
		if columns[k] || reBullet.MatchString(k) || reAltImage.MatchString(k) || strings.HasPrefix(k, "avAltImageName") {
			continue
		}
		out[AttrName(k)] = v
	}
	return out
}

// AttrName turns a feed field name into a label: avFlowRateGPM → "Flow Rate GPM",
// XV_Center_To_Center → "Center To Center", avNumHoles → "Num Holes".
func AttrName(k string) string {
	k = strings.TrimPrefix(k, "XV_")
	k = strings.TrimPrefix(k, "XL_")
	if strings.HasPrefix(k, "av") && len(k) > 2 && k[2] >= 'A' && k[2] <= 'Z' {
		k = k[2:]
	}
	if strings.HasPrefix(k, "at") && len(k) > 2 && k[2] >= 'A' && k[2] <= 'Z' {
		k = k[2:]
	}
	k = strings.ReplaceAll(k, "_", " ")
	// split camel case: FlowRateGPM → Flow Rate GPM
	var b strings.Builder
	rs := []rune(k)
	for i, c := range rs {
		if i > 0 && c >= 'A' && c <= 'Z' && rs[i-1] >= 'a' && rs[i-1] <= 'z' {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	return strings.TrimSpace(b.String())
}
