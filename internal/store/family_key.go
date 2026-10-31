package store

import (
	"context"
	"sort"
	"strings"
)

// siblingKey returns the family key for a SKU that is one of several finish variants printed
// together ("62858LF-PC / -PN / -SS" → "62858LF"; "9127-41-BP … -YP" → "9127-41").
// items are (catalog, page, sku) triples for one brand; the result maps sku → key for every
// SKU that qualifies.
func siblingKeys(items [][3]string) map[string]string {
	type grp struct {
		suffixes map[string]bool
		skus     []string
	}
	groups := map[string]*grp{} // catalog|page|prefix
	for _, it := range items {
		sku := strings.ToUpper(it[2])
		i := strings.LastIndex(sku, "-")
		if i < 4 {
			continue
		}
		prefix, suffix := sku[:i], sku[i+1:]
		if len(suffix) == 0 || len(suffix) > 6 || !hasDigit(prefix) {
			continue
		}
		if len(suffix) > 2 && !hasLetter(suffix) {
			continue // -100 / -200 are model numbers, not finishes
		}
		k := it[0] + "|" + it[1] + "|" + prefix
		g := groups[k]
		if g == nil {
			g = &grp{suffixes: map[string]bool{}}
			groups[k] = g
		}
		g.suffixes[suffix] = true
		g.skus = append(g.skus, sku)
	}
	out := map[string]string{}
	for k, g := range groups {
		if len(g.suffixes) < 2 {
			continue
		}
		prefix := k[strings.LastIndex(k, "|")+1:]
		for _, sku := range g.skus {
			out[sku] = prefix
		}
	}
	return out
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func hasLetter(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// finishCodeKey strips a brand's known finish code from the end of a SKU, whether printed
// with a separator ("62858LF-PC") or glued on ("RP71650NK", Brizo's accessory style), and
// returns the model key. codes must be sorted longest first so "BNX" wins over "BN".
// A match needs a model part of at least four characters that contains a digit, so a plain
// word or a short code is never mistaken for model+finish.
func finishCodeKey(sku string, codes []string) (string, bool) {
	u := strings.ToUpper(strings.TrimSpace(sku))
	for _, c := range codes {
		if len(c) == 0 || !strings.HasSuffix(u, c) {
			continue
		}
		model := strings.TrimRight(u[:len(u)-len(c)], "-/.")
		if len(model) < 4 || !hasDigit(model) {
			continue
		}
		// a glued code must follow a digit ("RP71650" + "NK"); after a letter it is part of
		// the model ("K30-123BL" could be a model suffix) unless a separator was printed
		last := model[len(model)-1]
		sep := len(model) < len(u)-len(c)
		if !sep && !(last >= '0' && last <= '9') {
			continue
		}
		return model, true
	}
	return "", false
}

// brandFinishCodes returns the finish codes known for a brand, longest first (brand rows,
// else the generic '*' map).
func (s *Store) brandFinishCodes(ctx context.Context, brandCode string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT upper(code) FROM finish_codes WHERE brand_code=$1
		UNION ALL
		SELECT upper(code) FROM finish_codes WHERE brand_code='*' AND NOT EXISTS (SELECT 1 FROM finish_codes x WHERE x.brand_code=$1)`, brandCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		if len(c) >= 2 {
			codes = append(codes, c)
		}
	}
	sort.Slice(codes, func(i, j int) bool { return len(codes[i]) > len(codes[j]) })
	return codes, rows.Err()
}

// brandSiblingKeys loads every price-book SKU for a brand and computes sibling keys: a SKU
// ending in a known finish code is keyed by its model; otherwise variants printed together on
// one page ("-PC / -PN / -SS") share the common prefix.
func (s *Store) brandSiblingKeys(ctx context.Context, brandCode string) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT i.catalog_id::text, i.page::text, i.sku FROM catalog_items i JOIN catalogs c ON c.id=i.catalog_id
		WHERE c.brand_code=$1`, brandCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items [][3]string
	for rows.Next() {
		var it [3]string
		if err := rows.Scan(&it[0], &it[1], &it[2]); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys := siblingKeys(items)
	codes, err := s.brandFinishCodes(ctx, brandCode)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if k, ok := finishCodeKey(it[2], codes); ok {
			keys[strings.ToUpper(it[2])] = k
		}
	}
	return keys, nil
}

// RefamilyCatalogProducts regroups catalog-sourced products whose SKUs are finish variants of
// one model into a single family, and drops families left empty. Run link/best/retitle after.
func (s *Store) RefamilyCatalogProducts(ctx context.Context, brandCode string) (moved, dropped int, err error) {
	brands, err := s.Pool.Query(ctx, `SELECT id, code FROM brands WHERE code IS NOT NULL AND ($1='' OR code=$1) ORDER BY code`, brandCode)
	if err != nil {
		return 0, 0, err
	}
	type br struct {
		id   int64
		code string
	}
	var list []br
	for brands.Next() {
		var b br
		if err := brands.Scan(&b.id, &b.code); err != nil {
			brands.Close()
			return 0, 0, err
		}
		list = append(list, b)
	}
	brands.Close()
	for _, b := range list {
		keys, err := s.brandSiblingKeys(ctx, b.code)
		if err != nil {
			return moved, dropped, err
		}
		if len(keys) == 0 {
			continue
		}
		rows, err := s.Pool.Query(ctx, `
			SELECT p.id, p.sku, f.id, f.family_key, COALESCE(f.title,''), COALESCE(f.room,'')
			FROM products p JOIN product_families f ON f.id=p.family_id
			WHERE p.brand_id=$1 AND p.source='catalog' AND f.source='catalog'`, b.id)
		if err != nil {
			return moved, dropped, err
		}
		type mv struct {
			pid, fid    int64
			sku, key    string
			title, room string
		}
		var todo []mv
		for rows.Next() {
			var m mv
			if err := rows.Scan(&m.pid, &m.sku, &m.fid, &m.key, &m.title, &m.room); err != nil {
				rows.Close()
				return moved, dropped, err
			}
			if nk, ok := keys[strings.ToUpper(m.sku)]; ok && nk != m.key {
				m.key = nk
				todo = append(todo, m)
			}
		}
		rows.Close()
		for _, m := range todo {
			var fid int64
			if err := s.Pool.QueryRow(ctx, `
				INSERT INTO product_families (brand_id, family_key, title, source, room, search)
				VALUES ($1,$2,$3,'catalog',NULLIF($4,''), to_tsvector('english', $3||' '||$2))
				ON CONFLICT (brand_id, family_key) DO UPDATE SET room = COALESCE(product_families.room, EXCLUDED.room)
				RETURNING id`, b.id, m.key, m.title, m.room).Scan(&fid); err != nil {
				return moved, dropped, err
			}
			if fid == m.fid {
				continue
			}
			if _, err := s.Pool.Exec(ctx, `UPDATE products SET family_id=$2 WHERE id=$1`, m.pid, fid); err != nil {
				return moved, dropped, err
			}
			// the product's images and documents follow it (vendor masters are keyed by SKU)
			if _, err := s.Pool.Exec(ctx, `UPDATE product_images SET family_id=$2 WHERE product_id=$1 AND family_id IS DISTINCT FROM $2`, m.pid, fid); err != nil {
				return moved, dropped, err
			}
			if _, err := s.Pool.Exec(ctx, `UPDATE documents SET family_id=$2 WHERE product_id=$1 AND family_id IS DISTINCT FROM $2`, m.pid, fid); err != nil {
				return moved, dropped, err
			}
			moved++
		}
	}
	tag, err := s.Pool.Exec(ctx, `
		DELETE FROM product_families f WHERE f.source='catalog'
		  AND NOT EXISTS (SELECT 1 FROM products p WHERE p.family_id=f.id)
		  AND NOT EXISTS (SELECT 1 FROM product_images i WHERE i.family_id=f.id)
		  AND NOT EXISTS (SELECT 1 FROM documents d WHERE d.family_id=f.id)`)
	if err != nil {
		return moved, dropped, err
	}
	return moved, int(tag.RowsAffected()), nil
}

// PriceProductsFromCatalogs sets each catalog product's list price from the newest price book
// that prints it (the lowest price on that book = the base finish), and clears the price of a
// catalog product no valid line prices any more. A quarantined product (price_flag set) keeps
// its price hidden until the flag is cleared by hand. Returns rows changed.
func (s *Store) PriceProductsFromCatalogs(ctx context.Context, brandCode string) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE products p SET price_cents = x.price
		FROM (
		  SELECT DISTINCT ON (b.id, i.sku) b.id AS brand_id, i.sku, i.price_cents AS price
		  FROM catalog_items i JOIN catalogs c ON c.id=i.catalog_id JOIN brands b ON b.code=c.brand_code
		  WHERE i.price_cents IS NOT NULL AND ($1='' OR b.code=$1)
		  ORDER BY b.id, i.sku, c.created_raw DESC NULLS LAST, i.price_cents ASC
		) x
		WHERE p.brand_id=x.brand_id AND p.sku=x.sku AND p.source='catalog' AND p.price_flag IS NULL
		  AND p.price_cents IS DISTINCT FROM x.price`, brandCode)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	tag, err = s.Pool.Exec(ctx, `
		UPDATE products p SET price_cents = NULL
		FROM brands b
		WHERE b.id=p.brand_id AND p.source='catalog' AND p.price_cents IS NOT NULL AND ($1='' OR b.code=$1)
		  AND NOT EXISTS (
		    SELECT 1 FROM catalog_items i JOIN catalogs c ON c.id=i.catalog_id
		    WHERE c.brand_code=b.code AND i.sku=p.sku AND i.price_cents IS NOT NULL)`, brandCode)
	if err != nil {
		return n, err
	}
	return n + int(tag.RowsAffected()), nil
}
