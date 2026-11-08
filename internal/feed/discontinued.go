package feed

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MarkDiscontinued reads Unilog's discontinuedProducts sheet (CSV export: brandName, brandCode,
// brandid, SKU, productID, productIndex, status, discDate) and flags the matching items.
// Returns rows read and items flagged.
func MarkDiscontinued(ctx context.Context, pool *pgxpool.Pool, path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	head, err := cr.Read()
	if err != nil {
		return 0, 0, err
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	bc, sc := col["brandcode"], col["sku"]
	if _, ok := col["brandcode"]; !ok {
		return 0, 0, fmt.Errorf("no brandCode column in %s", path)
	}
	if _, ok := col["sku"]; !ok {
		return 0, 0, fmt.Errorf("no SKU column in %s", path)
	}
	batch := &pgx.Batch{}
	n := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, 0, err
		}
		if len(rec) <= bc || len(rec) <= sc {
			continue
		}
		n++
		batch.Queue(`UPDATE items i SET discontinued = true FROM brands b WHERE b.id = i.brand_id AND b.code = $1 AND i.sku_norm = $2 AND NOT i.discontinued`,
			strings.TrimSpace(rec[bc]), NormSKU(rec[sc]))
	}
	br := pool.SendBatch(ctx, batch)
	flagged := 0
	for i := 0; i < batch.Len(); i++ {
		tag, err := br.Exec()
		if err != nil {
			br.Close()
			return n, flagged, err
		}
		flagged += int(tag.RowsAffected())
	}
	return n, flagged, br.Close()
}
