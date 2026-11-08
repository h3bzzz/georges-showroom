package store

import (
	"context"
	"strings"

	"georges/internal/taxonomy"
)

// finishCodes returns the brand's finish-code table (plus the shared '*' codes), code → finish.
func (s *Store) finishCodes(ctx context.Context, brand string) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT code, finish FROM finish_codes WHERE brand_code=$1 OR brand_code='*' ORDER BY (brand_code='*')`, brand)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := map[string]string{}
	for rows.Next() {
		var c, f string
		if err := rows.Scan(&c, &f); err != nil {
			return nil, err
		}
		if _, dup := codes[strings.ToUpper(c)]; !dup {
			codes[strings.ToUpper(c)] = f
		}
	}
	return codes, rows.Err()
}

func picturedFinish(codes map[string]string, u string) string {
	return taxonomy.PicturedFinish(codes, u)
}

func sameFinish(a, b string) bool { return taxonomy.SameFinish(a, b) }
