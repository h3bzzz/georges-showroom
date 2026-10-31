package store

import (
	"context"
	"os"
	"testing"
)

// TestCrawlQueue exercises enqueue → next → covered → done against the dev database.
// It is skipped unless DATABASE_URL is set (go test does not read .env).
func TestCrawlQueue(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := "https://www.georgesshowroom.com/Pasadena-California/Zz-test-"
	urls := []string{base + "A-1-Chrome-Faucet.HTM", base + "A-1-Nickel-Faucet.HTM", base + "B-2-Chrome-Sink.HTM"}
	defer st.Pool.Exec(ctx, `DELETE FROM crawl_queue WHERE url LIKE $1`, base+"%")
	items := []QueueItem{
		{URL: urls[0], Kind: "product", BrandSlug: "Zz-test"},
		{URL: urls[1], Kind: "product", BrandSlug: "Zz-test"},
		{URL: urls[2], Kind: "product", BrandSlug: "Zz-test"},
		{URL: urls[2], Kind: "product", BrandSlug: "Zz-test"}, // duplicate
	}
	n, err := st.EnqueueCrawl(ctx, items)
	if err != nil || n != 3 {
		t.Fatalf("enqueue n=%d err=%v", n, err)
	}
	if n, _ := st.EnqueueCrawl(ctx, items[:1]); n != 0 {
		t.Errorf("re-enqueue inserted %d", n)
	}
	next, err := st.NextCrawl(ctx, "zz-test", 3, 10)
	if err != nil || len(next) != 3 || next[0] != urls[0] {
		t.Fatalf("next = %v err=%v", next, err)
	}
	if err := st.MarkCrawled(ctx, urls[0]); err != nil {
		t.Fatal(err)
	}
	c, err := st.MarkCovered(ctx, []string{urls[1], base + "A-1-Brass-Faucet.HTM"}, urls[0], "Zz-test")
	if err != nil || c != 2 {
		t.Fatalf("covered = %d err=%v", c, err)
	}
	// a covered URL does not flip back to pending, and a done URL is not covered
	if c, _ := st.MarkCovered(ctx, []string{urls[0], urls[1]}, urls[2], ""); c != 0 {
		t.Errorf("recover changed %d rows", c)
	}
	next, _ = st.NextCrawl(ctx, "Zz-test", 3, 10)
	if len(next) != 1 || next[0] != urls[2] {
		t.Errorf("after cover next = %v", next)
	}
	for i := 0; i < 3; i++ {
		_ = st.MarkCrawlError(ctx, urls[2], "boom", false)
	}
	if next, _ = st.NextCrawl(ctx, "Zz-test", 3, 10); len(next) != 0 {
		t.Errorf("exhausted attempts still returned %v", next)
	}
}
