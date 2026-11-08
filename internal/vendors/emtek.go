package vendors

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Emtek (verified 2026-11-07): per-finish photographs exist on emtek.com but under Salsify
// names that cannot be derived from a SKU (random id + timestamp). A read-only crawl of the
// Wagtail page API (/api/v2/pages/<id>/ → attribute.finish_images) is kept in
// webimg/ETK/_finish_images.json; this source only looks the SKU up there. Rows carry the
// SKU printed in the file name (authoritative), its base model and its finish code.
type emtekSource struct {
	bySKU map[string]string
	byMF  map[string]string // model|code
}

var (
	etkCode  = regexp.MustCompile(`(US\d+[A-Z]*|MB|FB|TWB|SRG|MW|SCU|FBS|SWS|SS)$`)
	etkModel = regexp.MustCompile(`^[A-Z]?(\d{3,6})`)
)

func (s *emtekSource) init(_ context.Context, f *Fetcher) error {
	b, err := os.ReadFile(filepath.Join(f.DataDir, "webimg", "ETK", "_finish_images.json"))
	if err != nil {
		return err
	}
	var rows []struct {
		SKU, Model, FinishCode, URL string `json:",omitempty"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return err
	}
	s.bySKU, s.byMF = map[string]string{}, map[string]string{}
	for _, r := range rows {
		if r.SKU == "" || r.URL == "" {
			continue
		}
		k := strings.ToUpper(r.SKU)
		if _, dup := s.bySKU[k]; !dup {
			s.bySKU[k] = r.URL
		}
		if r.Model != "" && r.FinishCode != "" {
			mk := strings.ToUpper(r.Model) + "|" + strings.ToUpper(r.FinishCode)
			if _, dup := s.byMF[mk]; !dup {
				s.byMF[mk] = r.URL
			}
		}
	}
	return nil
}

func (s *emtekSource) candidates(sku string) (string, []string) {
	u := strings.ToUpper(strings.TrimSpace(sku))
	m := etkCode.FindStringSubmatch(u)
	if m == nil {
		return "", nil
	}
	code := m[1]
	if url, ok := s.bySKU[u]; ok {
		return code, []string{url}
	}
	// the maker's photo of the base model in this finish (rose and handing may differ)
	if n := etkModel.FindStringSubmatch(u); n != nil {
		if url, ok := s.byMF[n[1]+"|"+code]; ok {
			return code, []string{url}
		}
		// no photo in this finish: the same model in another finish still shows the design
		// (the store labels the picture with the finish its file name carries, never ours)
		for _, c := range etkPreferred {
			if url, ok := s.byMF[n[1]+"|"+c]; ok && c != code {
				return c, []string{url}
			}
		}
		for _, k := range s.modelKeys(n[1]) {
			return k[len(n[1])+1:], []string{s.byMF[k]}
		}
	}
	return "", nil
}

// etkPreferred orders the stand-in finishes: the catalog convention is chrome or satin nickel.
var etkPreferred = []string{"US26", "US15", "US26D", "US15A", "US10B", "US19", "MB", "FB", "US3", "US4", "US7", "US14"}

// modelKeys lists the model|code keys of one model, sorted for a stable choice.
func (s *emtekSource) modelKeys(model string) []string {
	var out []string
	for k := range s.byMF {
		if strings.HasPrefix(k, model+"|") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
func (s *emtekSource) valid(body []byte) bool { return len(body) > 8000 }
func (s *emtekSource) file(u, sku, _ string) string {
	// Salsify names are long and random; key the copy by our SKU
	ext := ".jpg"
	if i := strings.LastIndexByte(u, '.'); i >= 0 && len(u)-i <= 5 {
		ext = u[i:]
	}
	return strings.ToUpper(sku) + ext
}
