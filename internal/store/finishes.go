package store

import "context"

// ApplyFinishCodes fills products.finish for catalog SKUs from their suffix code (after the last
// '-', '/' or '.', optionally with trailing digits stripped: Barclay "ORB3"). Brand rows win;
// the generic '*' map is used only for brands that have no rows of their own. Returns rows set.
func (s *Store) ApplyFinishCodes(ctx context.Context, brandCode string) (int64, error) {
	var n int64
	for _, q := range []string{
		// exact suffix, brand-specific
		`UPDATE products p SET finish = fc.finish
		 FROM brands b, finish_codes fc
		 WHERE p.brand_id=b.id AND COALESCE(p.finish,'')='' AND ($1='' OR b.code=$1)
		   AND fc.brand_code=b.code AND fc.code = upper(regexp_replace(p.sku, '^.*[-/.]', ''))`,
		// suffix with trailing digits stripped, brand-specific
		`UPDATE products p SET finish = fc.finish
		 FROM brands b, finish_codes fc
		 WHERE p.brand_id=b.id AND COALESCE(p.finish,'')='' AND ($1='' OR b.code=$1)
		   AND fc.brand_code=b.code AND fc.code !~ '[0-9]$'
		   AND fc.code = upper(regexp_replace(regexp_replace(p.sku, '^.*[-/.]', ''), '[0-9]+$', ''))`,
		// glued suffix after a digit, brand-specific, longest code first ("RP71650BNX" → BNX not NK)
		`UPDATE products p SET finish = fc.finish
		 FROM brands b, finish_codes fc
		 WHERE p.brand_id=b.id AND COALESCE(p.finish,'')='' AND ($1='' OR b.code=$1)
		   AND fc.brand_code=b.code AND p.sku !~ '[-/.]' AND length(fc.code) >= 2
		   AND upper(p.sku) ~ ('[0-9]' || fc.code || '$')
		   AND NOT EXISTS (SELECT 1 FROM finish_codes x WHERE x.brand_code=b.code AND length(x.code) > length(fc.code)
		                   AND upper(p.sku) ~ ('[0-9]' || x.code || '$'))`,
		// generic map for brands without their own rows
		`UPDATE products p SET finish = fc.finish
		 FROM brands b, finish_codes fc
		 WHERE p.brand_id=b.id AND COALESCE(p.finish,'')='' AND ($1='' OR b.code=$1)
		   AND fc.brand_code='*' AND NOT EXISTS (SELECT 1 FROM finish_codes x WHERE x.brand_code=b.code)
		   AND fc.code = upper(regexp_replace(p.sku, '^.*[-/.]', ''))`,
	} {
		t, err := s.Pool.Exec(ctx, q, brandCode)
		if err != nil {
			return n, err
		}
		n += t.RowsAffected()
	}
	_, err := s.Pool.Exec(ctx, `SELECT refresh_family_blob()`)
	return n, err
}
