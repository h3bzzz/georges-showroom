package store

import (
	"context"
	"regexp"
	"strings"
)

var (
	rePlaceholder = regexp.MustCompile(`[.-]X{2,4}([.-].*)?$`)
	reTrailSep    = regexp.MustCompile(`[.-]+$`)
)

// SKUBase cuts a catalog token at its finish placeholder ("5399.XXX.D" → "5399",
// "K30-123-XX" → "K30-123") so it can be matched as a prefix of real SKUs.
func SKUBase(sku string) string {
	s := strings.ToUpper(strings.TrimSpace(sku))
	s = rePlaceholder.ReplaceAllString(s, "")
	return reTrailSep.ReplaceAllString(s, "")
}

// LinkCrops (re)builds family_crops from catalog_crops:
//
//	exact  – crop sku equals a product sku
//	family – crop base equals the family key
//	prefix – a product sku starts with the crop base (base ≥ 4 chars)
func (s *Store) LinkCrops(ctx context.Context) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE catalog_crops SET sku_base = NULLIF(regexp_replace(regexp_replace(upper(sku), '[.-]X{2,4}([.-].*)?$', ''), '[.-]+$', ''), '') WHERE sku IS NOT NULL`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM family_crops`); err != nil {
		return 0, err
	}
	// Links are scoped to the brand: a crop from a Baldwin book can only attach to Baldwin
	// families (catalogs.brand_code ↔ brands.code).
	const scope = ` JOIN catalogs c ON c.id=cc.catalog_id JOIN brands b ON b.code=c.brand_code `
	stmts := []string{
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT p.family_id, cc.id, 'exact' FROM catalog_crops cc` + scope + `JOIN products p ON p.brand_id=b.id AND p.sku = cc.sku
		 WHERE p.family_id IS NOT NULL ON CONFLICT DO NOTHING`,
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT f.id, cc.id, 'family' FROM catalog_crops cc` + scope + `JOIN product_families f ON f.brand_id=b.id AND f.family_key = cc.sku_base
		 WHERE cc.sku_base IS NOT NULL ON CONFLICT DO NOTHING`,
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT p.family_id, cc.id, 'prefix' FROM catalog_crops cc` + scope + `JOIN products p ON p.brand_id=b.id AND p.sku LIKE cc.sku_base || '%'
		 WHERE cc.sku_base IS NOT NULL AND length(cc.sku_base) >= 4 AND p.family_id IS NOT NULL ON CONFLICT DO NOTHING`,
		// punctuation-insensitive: "SH-162-10.25" in the book vs "SH-162-10-25" on the site
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT p.family_id, cc.id, 'norm' FROM catalog_crops cc` + scope + `JOIN products p ON p.brand_id=b.id AND p.sku_n LIKE cc.base_n || '%'
		 WHERE cc.base_n IS NOT NULL AND length(cc.base_n) >= 5 AND p.family_id IS NOT NULL ON CONFLICT DO NOTHING`,
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT f.id, cc.id, 'norm' FROM catalog_crops cc` + scope + `JOIN product_families f ON f.brand_id=b.id AND f.key_n = cc.base_n
		 WHERE cc.base_n IS NOT NULL AND length(cc.base_n) >= 5 ON CONFLICT DO NOTHING`,
		// suffix: the book prints the maker's model (BG-3000) and the price file prefixes it with
		// a vendor code (WI-BG3000); brand-scoped, six characters or more, so a bare "3000" never links
		`INSERT INTO family_crops (family_id, crop_id, how)
		 SELECT DISTINCT p.family_id, cc.id, 'suffix' FROM catalog_crops cc` + scope + `JOIN products p ON p.brand_id=b.id AND p.sku_n LIKE '%' || cc.base_n AND p.sku_n <> cc.base_n
		 WHERE cc.base_n IS NOT NULL AND length(cc.base_n) >= 6 AND p.family_id IS NOT NULL ON CONFLICT DO NOTHING`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q); err != nil {
			return 0, err
		}
	}
	// near: a price page prints a photo beside each block of SKUs (Robern: one drawing per size, a
	// column of models to its right) or above a table of them. A SKU whose row falls inside a
	// crop's vertical span, or that sits just under one, belongs to that crop — the way a reader
	// pairs them — so a page with six models still links each family to its own picture.
	if _, err := tx.Exec(ctx, `UPDATE catalog_skus SET sku_base = NULLIF(regexp_replace(regexp_replace(upper(sku), '[.-]X{2,4}([.-].*)?$', ''), '[.-]+$', ''), '') WHERE sku_base IS NULL`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO family_crops (family_id, crop_id, how)
		SELECT DISTINCT ON (fam.id) fam.id, cc.id, 'near'
		FROM catalog_skus k
		JOIN catalogs c ON c.id=k.catalog_id
		JOIN brands b ON b.code=c.brand_code
		JOIN product_families fam ON fam.brand_id=b.id
		JOIN products p ON p.family_id=fam.id
		JOIN catalog_crops cc ON cc.catalog_id=k.catalog_id AND cc.page=k.page AND cc.kind IN ('photo','drawing','table')
		WHERE k.sku_base IS NOT NULL AND length(k.sku_base) >= 4 AND k.w > 0
		  AND (p.sku = k.sku OR p.sku LIKE k.sku_base || '%' OR fam.family_key = k.sku_base
		       OR p.sku_n LIKE k.base_n || '%' OR fam.key_n = k.base_n)
		  AND NOT EXISTS (SELECT 1 FROM family_crops fc WHERE fc.family_id=fam.id AND fc.how IN ('exact','family','norm','suffix'))
		  AND (
		       -- beside: the SKU row lies within the crop's vertical span, no further than 450 px away
		       (k.y + k.h/2 BETWEEN cc.y - 20 AND cc.y + cc.h + 20
		        AND GREATEST(cc.x - (k.x + k.w), k.x - (cc.x + cc.w), 0) <= 450)
		       -- above: the crop ends within 500 px over the SKU and overlaps it horizontally
		    OR (cc.y + cc.h <= k.y AND k.y - (cc.y + cc.h) <= 500
		        AND LEAST(cc.x + cc.w, k.x + k.w) - GREATEST(cc.x, k.x) > 0)
		  )
		ORDER BY fam.id,
		  (k.y + k.h/2 BETWEEN cc.y - 20 AND cc.y + cc.h + 20) DESC,
		  (cc.kind='photo') DESC,
		  LEAST(abs(k.y - cc.y), abs(k.y - (cc.y + cc.h))) ASC,
		  cc.w*cc.h DESC
		ON CONFLICT DO NOTHING`); err != nil {
		return 0, err
	}
	// page: the family has no crop of its own, but one of its SKUs (or its key) is printed on a
	// page that has photos → take the largest photo on that page. Covers price-list layouts
	// where one image heads a table of many SKUs.
	if _, err := tx.Exec(ctx, `UPDATE catalog_skus SET sku_base = NULLIF(regexp_replace(regexp_replace(upper(sku), '[.-]X{2,4}([.-].*)?$', ''), '[.-]+$', ''), '') WHERE sku_base IS NULL`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO family_crops (family_id, crop_id, how)
		SELECT DISTINCT ON (fam.id) fam.id, cc.id, 'page'
		FROM catalog_skus k
		JOIN catalogs c ON c.id=k.catalog_id
		JOIN brands b ON b.code=c.brand_code
		JOIN product_families fam ON fam.brand_id=b.id
		JOIN products p ON p.family_id=fam.id
		JOIN catalog_crops cc ON cc.catalog_id=k.catalog_id AND cc.page=k.page
		WHERE k.sku_base IS NOT NULL AND length(k.sku_base) >= 4
		  AND (p.sku = k.sku OR p.sku LIKE k.sku_base || '%' OR fam.family_key = k.sku_base
		       OR p.sku_n LIKE k.base_n || '%' OR fam.key_n = k.base_n)
		  AND NOT EXISTS (SELECT 1 FROM family_crops fc WHERE fc.family_id=fam.id)
		ORDER BY fam.id, (cc.kind='photo') DESC, cc.w*cc.h DESC
		ON CONFLICT DO NOTHING`); err != nil {
		return 0, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM family_crops`).Scan(&n); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, s.RefreshFamilyBest(ctx)
}

// RefreshFamilyBest rebuilds family_best: the best photo (large, exact match, and shared by as
// few other families as possible so a grid does not repeat one collection shot), best drawing,
// web image and the catalog page that prints the SKU, per family.
func (s *Store) RefreshFamilyBest(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `
		WITH uses AS (
		  SELECT crop_id, count(DISTINCT family_id) AS n FROM family_crops GROUP BY crop_id
		), pages AS (
		  -- a 'page' link (same page, no SKU match) is trusted only on a page that shows one
		  -- photo or at most two models; on a 6-model price page it would give every family
		  -- the biggest picture on the page
		  SELECT cc.catalog_id, cc.page,
		    count(*) FILTER (WHERE cc.kind='photo') AS photos,
		    (SELECT count(DISTINCT k.sku_base) FROM catalog_skus k WHERE k.catalog_id=cc.catalog_id AND k.page=cc.page) AS models
		  FROM catalog_crops cc GROUP BY cc.catalog_id, cc.page
		), ranked AS (
		  -- the extractor files a product photo as a table or drawing when it sits in a price grid;
		  -- the vision model (migration 00029) is trusted to promote it at a confident score
		  SELECT fc.family_id,
		    CASE WHEN cc.kind <> 'photo' AND cc.vkind = 'photo' AND COALESCE(cc.vscore,0) >= 0.6 AND COALESCE(cc.quality,0) >= 0.2 THEN 'photo' ELSE cc.kind END AS kind,
		    COALESCE(cc.hd_path,cc.path) AS path, COALESCE(cc.hd_w,cc.w) AS w, cc.catalog_id, cc.page,
		    row_number() OVER (PARTITION BY fc.family_id, (CASE WHEN cc.kind <> 'photo' AND cc.vkind = 'photo' AND COALESCE(cc.vscore,0) >= 0.6 AND COALESCE(cc.quality,0) >= 0.2 THEN 'photo' ELSE cc.kind END) ORDER BY
		      (cc.w*cc.h >= 40000) DESC,
		      (fc.how IN ('exact','family','norm','suffix')) DESC,
		      (fc.how = 'near') DESC,
		      COALESCE(cc.quality, 0.3) DESC,
		      LEAST(u.n, 50) ASC,
		      cc.score DESC, cc.w*cc.h DESC) AS rn,
		    row_number() OVER (PARTITION BY fc.family_id ORDER BY (fc.how='exact') DESC, (fc.how IN ('family','norm')) DESC, cc.page) AS prn
		  FROM family_crops fc
		  JOIN catalog_crops cc ON cc.id=fc.crop_id
		  JOIN product_families pf ON pf.id=fc.family_id
		  JOIN uses u ON u.crop_id=fc.crop_id
		  JOIN pages pg ON pg.catalog_id=cc.catalog_id AND pg.page=cc.page
		  WHERE (fc.how <> 'page' OR pg.photos = 1 OR pg.models <= 2)
		    -- the vision model's subject must suit the family's category: a handle or a drain
		    -- never stands for a faucet, a tub never for a sink (migration 00029); the probe is ~89%
		    -- precise at 0.7 on the labelled set
		    AND (cc.vsubject IS NULL OR COALESCE(cc.vsubscore,0) < 0.7 OR subject_fits(cc.vsubject, pf.category_path))
		    -- a swatch, a logo, a person, a mood board: refused when the model is confident. At
		    -- milder scores (text/other 0.4, subject other 0.5) a 40-card audit found it wrong
		    -- half the time (Duravit vanities, Gessi spouts and flush plates read as "other")
		    AND NOT (cc.vkind IN ('text','other') AND COALESCE(cc.vscore,0) >= 0.6)
		    -- a detail shot or icon is never the card photo: a product crop on a 1700×2200 page is at
		    -- least ~150 px on its long side and ~80 px on its short side (a tidied tall faucet is 108×177)
		    AND ((cc.kind <> 'photo' AND (cc.vkind IS DISTINCT FROM 'photo' OR COALESCE(cc.quality,0) < 0.2)) OR (GREATEST(cc.w,cc.h) >= 150 AND LEAST(cc.w,cc.h) >= 80 AND cc.w*cc.h >= 12000))
		)
		INSERT INTO family_best (family_id, photo_path, photo_w, photo_how, drawing_path, web_path, page_catalog, page_no, updated_at)
		SELECT f.id, p.path, p.w, CASE WHEN p.path IS NULL THEN NULL ELSE 'crop' END, d.path,
		  (SELECT i.path FROM product_images i WHERE i.family_id=f.id AND i.source='web' AND i.path IS NOT NULL ORDER BY position LIMIT 1),
		  pg.catalog_id, pg.page, now()
		FROM product_families f
		LEFT JOIN ranked p  ON p.family_id=f.id  AND p.kind='photo'   AND p.rn=1
		LEFT JOIN ranked d  ON d.family_id=f.id  AND d.kind='drawing' AND d.rn=1
		LEFT JOIN ranked pg ON pg.family_id=f.id AND pg.prn=1
		ON CONFLICT (family_id) DO UPDATE SET photo_path=EXCLUDED.photo_path, photo_w=EXCLUDED.photo_w, photo_how=EXCLUDED.photo_how,
		  drawing_path=EXCLUDED.drawing_path, web_path=EXCLUDED.web_path, page_catalog=EXCLUDED.page_catalog,
		  page_no=EXCLUDED.page_no, updated_at=now()`)
	if err != nil {
		return err
	}
	// spec-sheet photo: a vendor's spec PDF often carries the product photograph itself
	// (Watermark, Emtek). One the vision model is sure is a photo whose subject suits the family
	// replaces a small or poor catalog crop, and gives a picture to a family that had none; a
	// vendor master still wins.
	_, err = s.Pool.Exec(ctx, `
		WITH spec AS (
		  SELECT DISTINCT ON (i.family_id) i.family_id, i.path, i.width
		  FROM product_images i JOIN product_families pf ON pf.id=i.family_id
		  WHERE i.source='spec' AND i.path IS NOT NULL AND i.vkind='photo' AND COALESCE(i.vscore,0) >= 0.8
		    AND COALESCE(i.width,0) >= 300 AND COALESCE(i.height,0) >= 200
		    AND (i.vsubject IS NULL OR COALESCE(i.vsubscore,0) < 0.7 OR subject_fits(i.vsubject, pf.category_path))
		  ORDER BY i.family_id, i.vscore DESC, i.width*i.height DESC
		)
		UPDATE family_best fb SET photo_path=spec.path, photo_w=spec.width, photo_how='spec', updated_at=now()
		FROM spec
		LEFT JOIN family_best cur ON cur.family_id=spec.family_id
		LEFT JOIN catalog_crops cc ON cur.photo_path IS NOT NULL AND (cc.path=cur.photo_path OR cc.hd_path=cur.photo_path)
		WHERE fb.family_id=spec.family_id AND fb.web_path IS NULL
		  AND (fb.photo_path IS NULL OR cc.w < 300 OR COALESCE(cc.quality,0) < 0.3)`)
	if err != nil {
		return err
	}
	// series photo: a price book opens each collection with a photograph a few pages before its
	// tables (Robern: the M Series room shot, then pages of cabinet drawings and SKUs). A family
	// with no picture of its own, whose SKU is printed on a page, takes the nearest such opener
	// above it: a page in the same book, at most four pages back, with one or two photographs,
	// that the vision model reads as a photograph (not a mood board, a swatch or a dimensioned
	// drawing), whose subject (when the model is sure) suits the family's category. A part or a
	// rough-in (tier 2, or a parts category) never borrows the collection shot: a drain is not
	// a sink and a cartridge is not a faucet.
	_, err = s.Pool.Exec(ctx, `
		WITH placed AS (
		  SELECT fb.family_id, fb.page_catalog AS catalog_id, fb.page_no AS page
		  FROM family_best fb JOIN product_families pf ON pf.id=fb.family_id
		  WHERE fb.photo_path IS NULL AND fb.web_path IS NULL AND fb.page_catalog IS NOT NULL AND series_ok(pf.tier, pf.category_path)
		  UNION
		  (SELECT DISTINCT ON (p.family_id) p.family_id, k.catalog_id, k.page
		   FROM family_best fb
		   JOIN products p ON p.family_id=fb.family_id
		   JOIN brands b ON b.id=p.brand_id
		   JOIN catalogs c ON c.brand_code=b.code
		   JOIN catalog_skus k ON k.catalog_id=c.id AND k.sku=p.sku
		   JOIN product_families pf ON pf.id=fb.family_id
		   WHERE fb.photo_path IS NULL AND fb.web_path IS NULL AND fb.page_catalog IS NULL AND series_ok(pf.tier, pf.category_path)
		   ORDER BY p.family_id, k.catalog_id, k.page)
		), openers AS (
		  SELECT cc.catalog_id, cc.page, cc.id, COALESCE(cc.hd_path,cc.path) AS path, COALESCE(cc.hd_w,cc.w) AS w, cc.w*cc.h AS area, cc.vsubject, cc.vsubscore
		  FROM catalog_crops cc
		  WHERE (cc.kind='photo' OR (cc.vkind='photo' AND COALESCE(cc.vscore,0) >= 0.6 AND COALESCE(cc.quality,0) >= 0.2)) AND cc.w >= 300 AND cc.h >= 200
		    AND (cc.vkind IS NULL OR cc.vkind = 'photo')
		    AND (SELECT count(*) FROM catalog_crops o WHERE o.catalog_id=cc.catalog_id AND o.page=cc.page AND o.kind='photo') <= 2
		), pick AS (
		  SELECT DISTINCT ON (pl.family_id) pl.family_id, o.path, o.w
		  FROM placed pl
		  JOIN product_families pf ON pf.id=pl.family_id
		  JOIN openers o ON o.catalog_id=pl.catalog_id AND o.page BETWEEN pl.page-4 AND pl.page
		  WHERE (o.vsubject IS NULL OR COALESCE(o.vsubscore,0) < 0.7 OR subject_fits(o.vsubject, pf.category_path))
		  ORDER BY pl.family_id, (pl.page - o.page) ASC, o.area DESC
		)
		UPDATE family_best fb SET photo_path=pick.path, photo_w=pick.w, photo_how='series', updated_at=now()
		FROM pick WHERE pick.family_id=fb.family_id`)
	if err != nil {
		return err
	}
	// per-family search blob (see migration 00019); families are matched against it by Browse/ListFamilies
	_, err = s.Pool.Exec(ctx, `SELECT refresh_family_blob()`)
	return err
}
