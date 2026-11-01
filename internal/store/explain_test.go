package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestExplainFinishQuery prints timings of the finish-search statements against the dev
// database (skipped without DATABASE_URL). Run: EXPLAIN=1 go test ./internal/store -run Explain -v
func TestExplainFinishQuery(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" || os.Getenv("EXPLAIN") == "" {
		t.Skip("needs DATABASE_URL and EXPLAIN=1")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := BrowseFilter{Query: os.Getenv("Q"), PerPage: 24, Page: 1}
	if f.Query == "" {
		f.Query = "antique rubbed bronze"
	}
	f.Intent = ParseIntentDict(f.Query, s.brandNames(ctx), s.finishDict(ctx))
	w, args := f.where("")
	for _, q := range []string{
		"SELECT count(*)" + browseFrom + w,
	} {
		st := time.Now()
		var n int
		if err := s.Pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("count=%d in %s", n, time.Since(st))
		rows, _ := s.Pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+q, args...)
		var plan []string
		for rows.Next() {
			var l string
			rows.Scan(&l)
			plan = append(plan, l)
		}
		rows.Close()
		t.Log("\n" + strings.Join(plan, "\n"))
	}
	st := time.Now()
	ids, err := s.rankFamilies(ctx, f, w, args)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rank %d ids in %s", len(ids), time.Since(st))
	st = time.Now()
	items, err := s.familiesByIDs(ctx, ids)
	t.Logf("hydrate %d in %s (%v)", len(items), time.Since(st), err)
	st = time.Now()
	err = s.preferFinishImages(ctx, items, nil, f.Intent.FinishConcept())
	t.Logf("images in %s (%v)", time.Since(st), err)
	for _, it := range items {
		t.Logf("item %d %-22s match=%-5s tone=%-6s finish=%-20s img=%s", it.ID, it.BrandName, it.ImageMatch, it.ImageTone, it.ImageFinish, it.Image)
	}
	for _, q := range []string{"category", "brand", "finish"} {
		cw, cargs := f.where(q)
		st = time.Now()
		var n int
		s.Pool.QueryRow(ctx, "SELECT count(*)"+browseFrom+cw, cargs...).Scan(&n)
		t.Logf("facet-where %s: %s", q, time.Since(st))
	}
}
