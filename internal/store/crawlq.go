package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// QueueItem is one URL to crawl. Kind follows sitemap.Kind (product | brand | category | other).
type QueueItem struct {
	URL       string
	Kind      string
	BrandSlug string
	Priority  int
}

// EnqueueCrawl adds URLs to crawl_queue, ignoring ones already present (whatever their status).
func (s *Store) EnqueueCrawl(ctx context.Context, items []QueueItem) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	urls := make([]string, len(items))
	kinds := make([]string, len(items))
	slugs := make([]*string, len(items))
	pris := make([]int16, len(items))
	for i, it := range items {
		urls[i], kinds[i], pris[i] = it.URL, it.Kind, int16(it.Priority)
		if it.BrandSlug != "" {
			v := it.BrandSlug
			slugs[i] = &v
		}
	}
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO crawl_queue (url, kind, brand_slug, priority)
		SELECT u, k, b, p FROM unnest($1::text[], $2::text[], $3::text[], $4::smallint[]) AS t(u, k, b, p)
		ON CONFLICT (url) DO NOTHING`, urls, kinds, slugs, pris)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// NextCrawl returns pending product URLs ordered so that the finish variants of one family are
// adjacent: after the first one is crawled its siblings get status 'covered' and drop out.
func (s *Store) NextCrawl(ctx context.Context, brandSlug string, maxAttempts, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT url FROM crawl_queue
		WHERE status='pending' AND kind='product' AND attempts < $2 AND ($1='' OR lower(brand_slug)=lower($1))
		ORDER BY priority DESC, brand_slug, url LIMIT $3`, brandSlug, maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) MarkCrawled(ctx context.Context, url string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO crawl_queue (url, kind, status, fetched_at, attempts) VALUES ($1,'product','done',now(),1)
		ON CONFLICT (url) DO UPDATE SET status='done', fetched_at=now(), last_error=NULL, attempts=crawl_queue.attempts+1`, url)
	return err
}

// MarkCrawlError records a failed attempt; fatal errors (not a product page) stop retries.
func (s *Store) MarkCrawlError(ctx context.Context, url, msg string, fatal bool) error {
	status := "pending"
	if fatal {
		status = "error"
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := s.Pool.Exec(ctx, `
		UPDATE crawl_queue SET attempts=attempts+1, last_error=$2, status=$3 WHERE url=$1`, url, msg, status)
	return err
}

// MarkCovered marks sibling finish pages (listed in the crawled page's finish dropdown) so the
// crawl visits one page per family. Only pending rows change; URLs not in the queue are added.
func (s *Store) MarkCovered(ctx context.Context, urls []string, by, brandSlug string) (int64, error) {
	if len(urls) == 0 {
		return 0, nil
	}
	var slug *string
	if brandSlug != "" {
		slug = &brandSlug
	}
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO crawl_queue (url, kind, status, covered_by, brand_slug)
		SELECT u, 'product', 'covered', $2, $3 FROM unnest($1::text[]) AS t(u)
		ON CONFLICT (url) DO UPDATE SET status='covered', covered_by=EXCLUDED.covered_by
		WHERE crawl_queue.status='pending'`, urls, by, slug)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type CrawlStat struct {
	Kind   string
	Status string
	N      int
	Brands int
	Last   *time.Time
}

func (s *Store) CrawlStats(ctx context.Context) ([]CrawlStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT kind, status, count(*)::int, count(DISTINCT brand_slug)::int, max(fetched_at)
		FROM crawl_queue GROUP BY kind, status ORDER BY kind, status`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CrawlStat])
}

// CrawlBrandStats lists pending/done counts per brand, biggest backlog first.
func (s *Store) CrawlBrandStats(ctx context.Context, limit int) ([]struct {
	Brand   string
	Pending int
	Done    int
	Covered int
	Errors  int
}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT COALESCE(brand_slug,'?'),
		  count(*) FILTER (WHERE status='pending')::int, count(*) FILTER (WHERE status='done')::int,
		  count(*) FILTER (WHERE status='covered')::int, count(*) FILTER (WHERE status='error')::int
		FROM crawl_queue WHERE kind='product' GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[struct {
		Brand   string
		Pending int
		Done    int
		Covered int
		Errors  int
	}])
}
