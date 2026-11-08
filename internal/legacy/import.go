// Package legacy imports the v1 database (product_families, products, catalog_crops,
// product_images, …) into the v2 schema. v1 stays the raw-data store that the crawler and the
// catalog pipeline write to; v2 is what the site reads. The import is idempotent: every row
// carries its legacy id or a natural key and is upserted.
package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"georges/internal/assets"
	"georges/internal/taxonomy"
)

type Importer struct {
	Src, Dst *pgxpool.Pool
	DataDir  string
	// Only is an optional subset of steps (brands, catalogs, families, items, documents,
	// assets, links, covers); empty = all.
	Only map[string]bool

	brandByLegacy map[int]int
	brandCode     map[int]string // new brand id → code
	catalogByLeg  map[int]int
	familyByLeg   map[int64]int64
	itemByLeg     map[int64]int64
	assetByCrop   map[int64]int64
	assetByWebImg map[int64]int64
}

func (im *Importer) want(step string) bool { return len(im.Only) == 0 || im.Only[step] }

// Run performs the import.
func (im *Importer) Run(ctx context.Context) error {
	im.brandByLegacy = map[int]int{}
	im.brandCode = map[int]string{}
	im.catalogByLeg = map[int]int{}
	im.familyByLeg = map[int64]int64{}
	im.itemByLeg = map[int64]int64{}
	im.assetByCrop = map[int64]int64{}
	im.assetByWebImg = map[int64]int64{}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"brands", im.brands}, {"catalogs", im.catalogs}, {"families", im.families}, {"items", im.items},
		{"documents", im.documents}, {"skus", im.skus}, {"assets", im.assets}, {"links", im.links}, {"covers", im.covers},
	}
	for _, s := range steps {
		if !im.want(s.name) {
			if err := im.loadMaps(ctx, s.name); err != nil {
				return err
			}
			continue
		}
		t := time.Now()
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("import %s: %w", s.name, err)
		}
		slog.Info("import step done", "step", s.name, "took", time.Since(t).Round(time.Millisecond))
	}
	return nil
}

// loadMaps fills the legacy→new id maps for a skipped step from what is already in v2.
func (im *Importer) loadMaps(ctx context.Context, step string) error {
	switch step {
	case "brands":
		rows, err := im.Dst.Query(ctx, `SELECT id, legacy_id, code FROM brands WHERE legacy_id IS NOT NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, leg int
			var code *string
			if err := rows.Scan(&id, &leg, &code); err != nil {
				return err
			}
			im.brandByLegacy[leg] = id
			if code != nil {
				im.brandCode[id] = *code
			}
		}
	case "catalogs":
		return scanMapInt(ctx, im.Dst, `SELECT id, legacy_id FROM catalogs WHERE legacy_id IS NOT NULL`, im.catalogByLeg)
	case "families":
		return scanMap64(ctx, im.Dst, `SELECT id, legacy_id FROM families WHERE legacy_id IS NOT NULL`, im.familyByLeg)
	case "items":
		return scanMap64(ctx, im.Dst, `SELECT id, legacy_id FROM items WHERE legacy_id IS NOT NULL`, im.itemByLeg)
	case "assets":
		if err := scanMap64(ctx, im.Dst, `SELECT id, (origin->>'crop_id')::bigint FROM assets WHERE origin ? 'crop_id'`, im.assetByCrop); err != nil {
			return err
		}
		return scanMap64(ctx, im.Dst, `SELECT id, (origin->>'image_id')::bigint FROM assets WHERE origin ? 'image_id'`, im.assetByWebImg)
	}
	return nil
}

func scanMapInt(ctx context.Context, p *pgxpool.Pool, q string, m map[int]int) error {
	rows, err := p.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, leg int
		if err := rows.Scan(&id, &leg); err != nil {
			return err
		}
		m[leg] = id
	}
	return rows.Err()
}

func scanMap64(ctx context.Context, p *pgxpool.Pool, q string, m map[int64]int64) error {
	rows, err := p.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, leg int64
		if err := rows.Scan(&id, &leg); err != nil {
			return err
		}
		m[leg] = id
	}
	return rows.Err()
}

// rel strips the legacy "data/raw/" prefix some rows carry so every path is DATA_DIR-relative.
func rel(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "data/raw/")
	return p
}

func (im *Importer) brands(ctx context.Context) error {
	rows, err := im.Src.Query(ctx, `SELECT id, code, slug, name, COALESCE(logo,''), COALESCE(featured,false), COALESCE(domain,''), COALESCE(price_multiplier,0.75) FROM brands`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type br struct {
		id         int
		code       *string
		slug, name string
		logo, dom  string
		featured   bool
		mult       float64
	}
	brs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (br, error) {
		var b br
		err := r.Scan(&b.id, &b.code, &b.slug, &b.name, &b.logo, &b.featured, &b.dom, &b.mult)
		return b, err
	})
	if err != nil {
		return err
	}
	for _, b := range brs {
		logo := rel(b.logo)
		if logo == "" {
			for _, ext := range []string{".png", ".svg", ".webp", ".jpg", ".gif"} {
				if _, err := os.Stat(filepath.Join(im.DataDir, "logos", b.slug+ext)); err == nil {
					logo = "logos/" + b.slug + ext
					break
				}
			}
		}
		room := taxonomy.BrandRooms[strings.ToLower(b.name)]
		b.slug = strings.ToLower(b.slug)
		var id int
		err := im.Dst.QueryRow(ctx, `
			INSERT INTO brands (code, slug, name, room, website, logo_path, featured, price_multiplier, legacy_id)
			VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,$9)
			ON CONFLICT (slug) DO UPDATE SET code=EXCLUDED.code, name=EXCLUDED.name, room=COALESCE(EXCLUDED.room, brands.room),
			  website=COALESCE(EXCLUDED.website, brands.website), logo_path=COALESCE(EXCLUDED.logo_path, brands.logo_path),
			  featured=EXCLUDED.featured, price_multiplier=EXCLUDED.price_multiplier, legacy_id=EXCLUDED.legacy_id
			RETURNING id`, b.code, b.slug, b.name, room, b.dom, logo, b.featured, b.mult, b.id).Scan(&id)
		if err != nil {
			return fmt.Errorf("brand %s: %w", b.slug, err)
		}
		im.brandByLegacy[b.id] = id
		if b.code != nil {
			im.brandCode[id] = *b.code
		}
	}
	// finish codes travel with the brands
	fc, err := im.Src.Query(ctx, `SELECT brand_code, code, finish FROM finish_codes`)
	if err != nil {
		return err
	}
	defer fc.Close()
	batch := &pgx.Batch{}
	for fc.Next() {
		var a, b, c string
		if err := fc.Scan(&a, &b, &c); err != nil {
			return err
		}
		batch.Queue(`INSERT INTO finish_codes (brand_code, code, finish) VALUES ($1,$2,$3) ON CONFLICT (brand_code, code) DO UPDATE SET finish=EXCLUDED.finish`, a, b, c)
	}
	fc.Close()
	return im.Dst.SendBatch(ctx, batch).Close()
}

func (im *Importer) catalogs(ctx context.Context) error {
	codeToBrand := map[string]int{}
	for id, code := range im.brandCode {
		codeToBrand[code] = id
	}
	rows, err := im.Src.Query(ctx, `SELECT id, brand_code, name, COALESCE(title,''), COALESCE(pages,0), COALESCE(has_text,false) FROM catalogs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cat struct {
		id         int
		code, name string
		title      string
		pages      int
		hasText    bool
	}
	cats, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (cat, error) {
		var c cat
		err := r.Scan(&c.id, &c.code, &c.name, &c.title, &c.pages, &c.hasText)
		return c, err
	})
	if err != nil {
		return err
	}
	for _, c := range cats {
		var brandID *int
		if id, ok := codeToBrand[c.code]; ok {
			brandID = &id
		}
		cover := ""
		if _, err := os.Stat(filepath.Join(im.DataDir, "catalogs", c.code, c.name, "0001.jpg")); err == nil {
			cover = fmt.Sprintf("catalogs/%s/%s/0001.jpg", c.code, c.name)
		}
		var id int
		err := im.Dst.QueryRow(ctx, `
			INSERT INTO catalogs (brand_id, code, name, title, pages, has_text, cover_path, legacy_id)
			VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,NULLIF($7,''),$8)
			ON CONFLICT (code, name) DO UPDATE SET brand_id=EXCLUDED.brand_id, title=EXCLUDED.title, pages=EXCLUDED.pages,
			  has_text=EXCLUDED.has_text, cover_path=COALESCE(EXCLUDED.cover_path, catalogs.cover_path), legacy_id=EXCLUDED.legacy_id
			RETURNING id`, brandID, c.code, c.name, c.title, c.pages, c.hasText, cover, c.id).Scan(&id)
		if err != nil {
			return err
		}
		im.catalogByLeg[c.id] = id
	}
	// pages: the flipbook text layer when the book has one, else the OCR text
	pr, err := im.Src.Query(ctx, `SELECT catalog_id, page, COALESCE(path,''), COALESCE(NULLIF(text,''), ocr_text, '') FROM catalog_pages`)
	if err != nil {
		return err
	}
	defer pr.Close()
	batch := &pgx.Batch{}
	n := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		err := im.Dst.SendBatch(ctx, batch).Close()
		batch = &pgx.Batch{}
		return err
	}
	for pr.Next() {
		var cid, page int
		var path, text string
		if err := pr.Scan(&cid, &page, &path, &text); err != nil {
			return err
		}
		nid, ok := im.catalogByLeg[cid]
		if !ok {
			continue
		}
		batch.Queue(`INSERT INTO catalog_pages (catalog_id, page, path, text) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''))
			ON CONFLICT (catalog_id, page) DO UPDATE SET path=EXCLUDED.path, text=EXCLUDED.text`, nid, page, rel(path), text)
		n++
		if batch.Len() >= 200 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	pr.Close()
	if err := flush(); err != nil {
		return err
	}
	tr, err := im.Src.Query(ctx, `SELECT catalog_id, page, title, level, source FROM catalog_toc`)
	if err != nil {
		return err
	}
	defer tr.Close()
	for tr.Next() {
		var cid, page, level int
		var title, source string
		if err := tr.Scan(&cid, &page, &title, &level, &source); err != nil {
			return err
		}
		if nid, ok := im.catalogByLeg[cid]; ok {
			batch.Queue(`INSERT INTO catalog_toc (catalog_id, page, title, level, source) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, nid, page, title, level, source)
		}
		if batch.Len() >= 500 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	tr.Close()
	slog.Info("catalogs", "books", len(cats), "pages", n)
	return flush()
}

var reSKUNorm = regexp.MustCompile(`[^A-Z0-9]+`)

// NormSKU is the join key across sources: upper case, letters and digits only.
func NormSKU(s string) string { return reSKUNorm.ReplaceAllString(strings.ToUpper(s), "") }

type fact struct {
	Value  string  `json:"value"`
	Unit   string  `json:"unit,omitempty"`
	Source string  `json:"source,omitempty"`
	Conf   float32 `json:"conf,omitempty"`
}

func (im *Importer) families(ctx context.Context) error {
	// facts first, grouped per family
	facts := map[int64]map[string]fact{}
	fr, err := im.Src.Query(ctx, `SELECT family_id, key, value, COALESCE(unit,''), source, COALESCE(conf,0) FROM family_facts ORDER BY family_id, key, (source='site') DESC, conf DESC`)
	if err != nil {
		return err
	}
	defer fr.Close()
	for fr.Next() {
		var fid int64
		var key, val, unit, source string
		var conf float32
		if err := fr.Scan(&fid, &key, &val, &unit, &source, &conf); err != nil {
			return err
		}
		if facts[fid] == nil {
			facts[fid] = map[string]fact{}
		}
		if _, dup := facts[fid][key]; !dup {
			facts[fid][key] = fact{val, unit, source, conf}
		}
	}
	fr.Close()
	finishes := map[int64][]string{}
	pr, err := im.Src.Query(ctx, `SELECT family_id, finish FROM products WHERE family_id IS NOT NULL AND COALESCE(finish,'')<>'' GROUP BY 1,2 ORDER BY 1, count(*) DESC`)
	if err != nil {
		return err
	}
	defer pr.Close()
	for pr.Next() {
		var fid int64
		var fin string
		if err := pr.Scan(&fid, &fin); err != nil {
			return err
		}
		finishes[fid] = append(finishes[fid], fin)
	}
	pr.Close()
	prices := map[int64][2]*int64{}
	qr, err := im.Src.Query(ctx, `SELECT family_id, min(price_cents), max(price_cents) FROM products WHERE family_id IS NOT NULL AND price_cents IS NOT NULL AND price_flag IS NULL GROUP BY 1`)
	if err != nil {
		return err
	}
	defer qr.Close()
	for qr.Next() {
		var fid int64
		var lo, hi *int64
		if err := qr.Scan(&fid, &lo, &hi); err != nil {
			return err
		}
		prices[fid] = [2]*int64{lo, hi}
	}
	qr.Close()
	// the SKU that names the family, for the brand prefix rules
	firstSKU := map[int64]string{}
	sr, err := im.Src.Query(ctx, `SELECT DISTINCT ON (family_id) family_id, sku FROM products WHERE family_id IS NOT NULL ORDER BY family_id, id`)
	if err != nil {
		return err
	}
	defer sr.Close()
	for sr.Next() {
		var fid int64
		var sku string
		if err := sr.Scan(&fid, &sku); err != nil {
			return err
		}
		firstSKU[fid] = sku
	}
	sr.Close()

	rows, err := im.Src.Query(ctx, `
		SELECT f.id, f.brand_id, f.family_key, COALESCE(f.title,''), COALESCE(f.description,''), COALESCE(f.product_type,''),
		       COALESCE(f.collection,''), COALESCE(f.category_path,'{}'), f.attributes, COALESCE(f.bullets,'{}'), COALESCE(f.room,''), f.source, b.name
		FROM product_families f JOIN brands b ON b.id=f.brand_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	type pendingFam struct {
		leg int64
	}
	var queued []int64
	n := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		res := im.Dst.SendBatch(ctx, batch)
		for _, leg := range queued {
			var id int64
			if err := res.QueryRow().Scan(&id); err != nil {
				res.Close()
				return fmt.Errorf("family %d: %w", leg, err)
			}
			im.familyByLeg[leg] = id
		}
		if err := res.Close(); err != nil {
			return err
		}
		batch, queued = &pgx.Batch{}, queued[:0]
		return nil
	}
	for rows.Next() {
		var leg int64
		var brandLeg int
		var key, title, desc, ptype, coll, room, source, brandName string
		var cats, bullets []string
		var attrs []byte
		if err := rows.Scan(&leg, &brandLeg, &key, &title, &desc, &ptype, &coll, &cats, &attrs, &bullets, &room, &source, &brandName); err != nil {
			return err
		}
		brandID, ok := im.brandByLegacy[brandLeg]
		if !ok {
			continue
		}
		code := im.brandCode[brandID]
		category := ""
		if len(cats) > 0 {
			category = cats[0]
		}
		if room == "" {
			room = taxonomy.RoomFor(category, ptype, title, brandName)
		}
		if category == "" && source == "catalog" {
			category = taxonomy.CategoryFor(code, firstSKU[leg], room, title, ptype)
		}
		if f, ok := facts[leg]["Collection"]; ok && coll == "" {
			coll = f.Value
		}
		tier := taxonomy.TierFor(room, title, ptype, category)
		fj, _ := json.Marshal(facts[leg])
		if facts[leg] == nil {
			fj = []byte("{}")
		}
		if len(attrs) == 0 {
			attrs = []byte("{}")
		}
		var lo, hi *int64
		if p, ok := prices[leg]; ok {
			lo, hi = p[0], p[1]
		}
		fins := finishes[leg]
		if fins == nil {
			fins = []string{}
		}
		if bullets == nil {
			bullets = []string{}
		}
		batch.Queue(`
			INSERT INTO families (brand_id, key, title, collection, category, room, tier, description, bullets, attributes, facts, finishes, source, price_min_cents, price_max_cents, legacy_id, updated_at)
			VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),$9,$10,$11,$12,$13,$14,$15,$16,now())
			ON CONFLICT (brand_id, key) DO UPDATE SET title=EXCLUDED.title, collection=EXCLUDED.collection, category=EXCLUDED.category, room=EXCLUDED.room,
			  tier=EXCLUDED.tier, description=EXCLUDED.description, bullets=EXCLUDED.bullets, attributes=EXCLUDED.attributes, facts=EXCLUDED.facts,
			  finishes=EXCLUDED.finishes, source=EXCLUDED.source, price_min_cents=EXCLUDED.price_min_cents, price_max_cents=EXCLUDED.price_max_cents,
			  legacy_id=EXCLUDED.legacy_id, updated_at=now()
			RETURNING id`,
			brandID, key, title, coll, category, room, tier, desc, bullets, string(attrs), string(fj), fins, source, lo, hi, leg)
		queued = append(queued, leg)
		n++
		if batch.Len() >= 500 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	rows.Close()
	if err := flush(); err != nil {
		return err
	}
	slog.Info("families", "rows", n)
	return nil
}

func (im *Importer) items(ctx context.Context) error {
	rows, err := im.Src.Query(ctx, `
		SELECT id, brand_id, family_id, sku, COALESCE(upc,''), COALESCE(finish,''), COALESCE(url,''), price_cents, msrp_cents, lead_time_days, price_flag, source
		FROM products WHERE family_id IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	var queued []int64
	n := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		res := im.Dst.SendBatch(ctx, batch)
		for _, leg := range queued {
			var id int64
			if err := res.QueryRow().Scan(&id); err != nil {
				res.Close()
				return fmt.Errorf("item %d: %w", leg, err)
			}
			im.itemByLeg[leg] = id
		}
		if err := res.Close(); err != nil {
			return err
		}
		batch, queued = &pgx.Batch{}, queued[:0]
		return nil
	}
	for rows.Next() {
		var leg, famLeg int64
		var brandLeg int
		var sku, upc, finish, url, source string
		var price, list *int64
		var lead *int
		var flag *string
		if err := rows.Scan(&leg, &brandLeg, &famLeg, &sku, &upc, &finish, &url, &price, &list, &lead, &flag, &source); err != nil {
			return err
		}
		brandID, ok := im.brandByLegacy[brandLeg]
		famID, ok2 := im.familyByLeg[famLeg]
		if !ok || !ok2 || strings.TrimSpace(sku) == "" {
			continue
		}
		batch.Queue(`
			INSERT INTO items (family_id, brand_id, sku, sku_norm, finish, upc, list_cents, price_cents, price_flag, lead_time_days, url, source, legacy_id)
			VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,NULLIF($11,''),$12,$13)
			ON CONFLICT (brand_id, sku) DO UPDATE SET family_id=EXCLUDED.family_id, sku_norm=EXCLUDED.sku_norm, finish=EXCLUDED.finish, upc=EXCLUDED.upc,
			  list_cents=EXCLUDED.list_cents, price_cents=EXCLUDED.price_cents, price_flag=EXCLUDED.price_flag, lead_time_days=EXCLUDED.lead_time_days,
			  url=EXCLUDED.url, source=EXCLUDED.source, legacy_id=EXCLUDED.legacy_id
			RETURNING id`,
			famID, brandID, sku, NormSKU(sku), finish, upc, list, price, flag, lead, url, source, leg)
		queued = append(queued, leg)
		n++
		if batch.Len() >= 500 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	rows.Close()
	if err := flush(); err != nil {
		return err
	}
	slog.Info("items", "rows", n)
	return nil
}

func (im *Importer) documents(ctx context.Context) error {
	rows, err := im.Src.Query(ctx, `SELECT id, family_id, product_id, kind, url, COALESCE(path,''), pages, COALESCE(text,'') FROM documents WHERE error IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	n := 0
	for rows.Next() {
		var leg int64
		var famLeg, prodLeg *int64
		var kind, url, path, text string
		var pages *int
		if err := rows.Scan(&leg, &famLeg, &prodLeg, &kind, &url, &path, &pages, &text); err != nil {
			return err
		}
		var famID, itemID *int64
		if famLeg != nil {
			if id, ok := im.familyByLeg[*famLeg]; ok {
				famID = &id
			}
		}
		if prodLeg != nil {
			if id, ok := im.itemByLeg[*prodLeg]; ok {
				itemID = &id
			}
		}
		batch.Queue(`INSERT INTO documents (family_id, item_id, kind, url, path, pages, text, legacy_id) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),$8)
			ON CONFLICT (url) DO UPDATE SET family_id=EXCLUDED.family_id, item_id=EXCLUDED.item_id, kind=EXCLUDED.kind, path=EXCLUDED.path, pages=EXCLUDED.pages, text=EXCLUDED.text, legacy_id=EXCLUDED.legacy_id`,
			famID, itemID, kind, url, rel(path), pages, text, leg)
		n++
	}
	rows.Close()
	if err := im.Dst.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	slog.Info("documents", "rows", n)
	return nil
}

// assetRow is one picture to register.
type assetRow struct {
	srcPath   string
	source    string
	origin    map[string]any
	kind      *string
	kindScore *float32
	subject   *string
	subScore  *float32
	scene     *float32
	tone      *string
	toneScore *float32
	emb       []byte
	embModel  *string
	rejected  bool
	legKey    string // "crop:<id>" | "img:<id>"
	legID     int64
	// filled by the hasher
	hash  string
	bytes int64
}

func (im *Importer) assets(ctx context.Context) error {
	var all []*assetRow
	// catalog crops: the upscaled file when one exists, the raw crop otherwise
	cr, err := im.Src.Query(ctx, `
		SELECT cc.id, cc.catalog_id, cc.page, cc.x, cc.y, cc.w, cc.h, cc.path, cc.hd_path, cc.hd_w, cc.sku, cc.kind, cc.pkind, cc.quality,
		       cc.vkind, cc.vscore, cc.vsubject, cc.vsubscore, cc.vscene, cc.vtone, cc.vtonescore, cc.vemb, cc.vmodel, cc.rejected, cc.dup_of
		FROM catalog_crops cc`)
	if err != nil {
		return err
	}
	defer cr.Close()
	for cr.Next() {
		var id, catLeg int64
		var page, x, y, w, h int
		var path string
		var hdPath, sku, kind, pkind, vkind, vsubject, vtone, vmodel *string
		var hdW *int
		var quality, vscore, vsubscore, vscene, vtonescore *float32
		var vemb []byte
		var rejected bool
		var dupOf *int64
		if err := cr.Scan(&id, &catLeg, &page, &x, &y, &w, &h, &path, &hdPath, &hdW, &sku, &kind, &pkind, &quality,
			&vkind, &vscore, &vsubject, &vsubscore, &vscene, &vtone, &vtonescore, &vemb, &vmodel, &rejected, &dupOf); err != nil {
			return err
		}
		src := rel(path)
		hd := false
		if hdPath != nil && hdW != nil && *hdW > 0 {
			src = rel(*hdPath)
			hd = true
		}
		origin := map[string]any{"crop_id": id, "catalog_id": im.catalogByLeg[int(catLeg)], "page": page,
			"x": x, "y": y, "w": w, "h": h, "hd": hd}
		if hd {
			origin["hd_w"] = *hdW
		}
		if sku != nil {
			origin["sku"] = *sku
		}
		if kind != nil {
			origin["lkind"] = *kind
		}
		if pkind != nil {
			origin["pkind"] = *pkind
		}
		if quality != nil {
			origin["lq"] = *quality
		}
		if dupOf != nil {
			origin["dup_of_crop"] = *dupOf
		}
		all = append(all, &assetRow{srcPath: src, source: "catalog", origin: origin, kind: vkind, kindScore: vscore,
			subject: vsubject, subScore: vsubscore, scene: vscene, tone: vtone, toneScore: vtonescore, emb: vemb, embModel: vmodel,
			rejected: rejected, legKey: "crop", legID: id})
	}
	cr.Close()
	// vendor masters and spec-sheet rasters
	ir, err := im.Src.Query(ctx, `
		SELECT id, product_id, family_id, source, COALESCE(url,''), path, width, height, vkind, vscore, vsubject, vsubscore, vemb, vmodel, vtone
		FROM product_images WHERE path IS NOT NULL AND path NOT LIKE 'miss:%' AND source IN ('web','spec')`)
	if err != nil {
		return err
	}
	defer ir.Close()
	for ir.Next() {
		var id int64
		var prodLeg, famLeg *int64
		var source, url, path string
		var w, h *int
		var vkind, vsubject, vmodel, vtone *string
		var vscore, vsubscore *float32
		var vemb []byte
		if err := ir.Scan(&id, &prodLeg, &famLeg, &source, &url, &path, &w, &h, &vkind, &vscore, &vsubject, &vsubscore, &vemb, &vmodel, &vtone); err != nil {
			return err
		}
		origin := map[string]any{"image_id": id}
		if w != nil && h != nil && *w > 0 && *h > 0 {
			origin["w"], origin["h"] = *w, *h
		}
		if url != "" {
			origin["url"] = url
		}
		if prodLeg != nil {
			origin["item_leg"] = *prodLeg
		}
		if famLeg != nil {
			origin["family_leg"] = *famLeg
		}
		src := "vendor"
		if source == "spec" {
			src = "spec"
		}
		all = append(all, &assetRow{srcPath: rel(path), source: src, origin: origin, kind: vkind, kindScore: vscore,
			subject: vsubject, subScore: vsubscore, tone: vtone, emb: vemb, embModel: vmodel, legKey: "img", legID: id})
	}
	ir.Close()
	slog.Info("assets: hashing files", "count", len(all))

	// hash in parallel; missing files are dropped
	var wg sync.WaitGroup
	jobs := make(chan *assetRow)
	var missing, mu = 0, sync.Mutex{}
	for i := 0; i < max(2, runtime.NumCPU()); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				abs, err := assets.Abs(im.DataDir, a.srcPath)
				if err == nil {
					a.hash, a.bytes, err = assets.HashFile(abs)
				}
				if err != nil {
					mu.Lock()
					missing++
					mu.Unlock()
					a.hash = ""
				}
			}
		}()
	}
	for _, a := range all {
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	slog.Info("assets: hashed", "missing", missing)

	batch := &pgx.Batch{}
	var queued []*assetRow
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		res := im.Dst.SendBatch(ctx, batch)
		for _, a := range queued {
			var id int64
			if err := res.QueryRow().Scan(&id); err != nil {
				res.Close()
				return fmt.Errorf("asset %s: %w", a.srcPath, err)
			}
			if a.legKey == "crop" {
				im.assetByCrop[a.legID] = id
			} else {
				im.assetByWebImg[a.legID] = id
			}
		}
		if err := res.Close(); err != nil {
			return err
		}
		batch, queued = &pgx.Batch{}, queued[:0]
		return nil
	}
	n := 0
	for _, a := range all {
		if a.hash == "" {
			continue
		}
		oj, _ := json.Marshal(a.origin)
		// an identical file registered twice (the same shot cut from two books) is one asset;
		// the no-op update makes RETURNING yield the existing row
		batch.Queue(`
			INSERT INTO assets (hash, src_path, bytes, source, origin, kind, kind_score, subject, subject_score, scene, tone, tone_score, embedding, emb_model, rejected)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (hash) DO UPDATE SET rejected = assets.rejected OR EXCLUDED.rejected,
			  kind = COALESCE(assets.kind, EXCLUDED.kind), kind_score = COALESCE(assets.kind_score, EXCLUDED.kind_score),
			  subject = COALESCE(assets.subject, EXCLUDED.subject), subject_score = COALESCE(assets.subject_score, EXCLUDED.subject_score),
			  embedding = COALESCE(assets.embedding, EXCLUDED.embedding), emb_model = COALESCE(assets.emb_model, EXCLUDED.emb_model)
			RETURNING id`,
			a.hash, a.srcPath, a.bytes, a.source, string(oj), a.kind, a.kindScore, a.subject, a.subScore, a.scene, a.tone, a.toneScore, a.emb, a.embModel, a.rejected)
		queued = append(queued, a)
		n++
		if batch.Len() >= 500 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	// duplicates found by the legacy CLIP pass carry over as dup_of
	dupBatch := &pgx.Batch{}
	for _, a := range all {
		if a.hash == "" || a.legKey != "crop" {
			continue
		}
		if d, ok := a.origin["dup_of_crop"].(int64); ok {
			if to, ok := im.assetByCrop[d]; ok {
				if from, ok := im.assetByCrop[a.legID]; ok && from != to {
					dupBatch.Queue(`UPDATE assets SET dup_of=$2 WHERE id=$1 AND dup_of IS NULL`, from, to)
				}
			}
		}
	}
	if dupBatch.Len() > 0 {
		if err := im.Dst.SendBatch(ctx, dupBatch).Close(); err != nil {
			return err
		}
	}
	// provisional size from the crop geometry (x4 when the upscaled file is the source) so the
	// picker can apply its size rules before `assets build` measures the real pixels
	_, err = im.Dst.Exec(ctx, `
		UPDATE assets SET
		  width  = COALESCE((origin->>'hd_w')::int, (origin->>'w')::int),
		  height = GREATEST(1, round((origin->>'h')::numeric * COALESCE((origin->>'hd_w')::numeric, (origin->>'w')::numeric) / NULLIF((origin->>'w')::numeric, 0)))::int
		WHERE width IS NULL AND origin ? 'w' AND (origin->>'w')::int > 0`)
	if err != nil {
		return err
	}
	slog.Info("assets", "rows", n, "dups", dupBatch.Len())
	return nil
}

func (im *Importer) links(ctx context.Context) error {
	if _, err := im.Dst.Exec(ctx, `DELETE FROM asset_links`); err != nil {
		return err
	}
	rows, err := im.Src.Query(ctx, `SELECT family_id, crop_id, how FROM family_crops`)
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	n := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		err := im.Dst.SendBatch(ctx, batch).Close()
		batch = &pgx.Batch{}
		return err
	}
	for rows.Next() {
		var famLeg, cropLeg int64
		var how string
		if err := rows.Scan(&famLeg, &cropLeg, &how); err != nil {
			return err
		}
		fid, ok := im.familyByLeg[famLeg]
		aid, ok2 := im.assetByCrop[cropLeg]
		if !ok || !ok2 {
			continue
		}
		if how == "prefix" {
			how = "family"
		}
		batch.Queue(`INSERT INTO asset_links (asset_id, family_id, how, weight) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, aid, fid, how, linkWeight(how))
		n++
		if batch.Len() >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	rows.Close()
	if err := flush(); err != nil {
		return err
	}
	// vendor masters and spec rasters link to their product's family and item
	ir, err := im.Src.Query(ctx, `SELECT id, product_id, family_id, source FROM product_images WHERE path IS NOT NULL AND path NOT LIKE 'miss:%' AND source IN ('web','spec')`)
	if err != nil {
		return err
	}
	defer ir.Close()
	for ir.Next() {
		var id int64
		var prodLeg, famLeg *int64
		var source string
		if err := ir.Scan(&id, &prodLeg, &famLeg, &source); err != nil {
			return err
		}
		aid, ok := im.assetByWebImg[id]
		if !ok {
			continue
		}
		var fid, iid *int64
		if famLeg != nil {
			if v, ok := im.familyByLeg[*famLeg]; ok {
				fid = &v
			}
		}
		if prodLeg != nil {
			if v, ok := im.itemByLeg[*prodLeg]; ok {
				iid = &v
			}
		}
		if fid == nil && iid != nil {
			var v int64
			if err := im.Dst.QueryRow(ctx, `SELECT family_id FROM items WHERE id=$1`, *iid).Scan(&v); err == nil {
				fid = &v
			}
		}
		if fid == nil {
			continue
		}
		how := "vendor"
		if source == "spec" {
			how = "spec"
		}
		batch.Queue(`INSERT INTO asset_links (asset_id, family_id, item_id, how, weight) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, aid, *fid, iid, how, linkWeight(how))
		n++
		if batch.Len() >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	ir.Close()
	if err := flush(); err != nil {
		return err
	}
	// a crop printed next to a SKU also links to that item when the family has it
	_, err = im.Dst.Exec(ctx, `
		UPDATE asset_links al SET item_id = i.id
		FROM assets a, items i
		WHERE al.asset_id = a.id AND al.item_id IS NULL AND a.origin ? 'sku'
		  AND i.family_id = al.family_id AND i.sku_norm = regexp_replace(upper(a.origin->>'sku'), '[^A-Z0-9]', '', 'g')`)
	if err != nil {
		return err
	}
	slog.Info("links", "rows", n)
	return nil
}

// linkWeight orders evidence: a vendor master keyed by SKU is certain, a page-level link weak.
func linkWeight(how string) float32 {
	switch how {
	case "vendor":
		return 1.0
	case "sku", "exact":
		return 0.95
	case "spec":
		return 0.9
	case "family", "norm", "suffix":
		return 0.8
	case "near":
		return 0.6
	case "page":
		return 0.3
	}
	return 0.5
}

// skus copies where every SKU is printed in the catalogs (positions on the page render).
func (im *Importer) skus(ctx context.Context) error {
	if _, err := im.Dst.Exec(ctx, `TRUNCATE catalog_skus`); err != nil {
		return err
	}
	rows, err := im.Src.Query(ctx, `SELECT catalog_id, page, sku, COALESCE(x,0), COALESCE(y,0), COALESCE(w,0), COALESCE(h,0) FROM catalog_skus`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		cid, page, x, y, w, h int
		sku                   string
	}
	var buf []row
	seen := map[string]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.cid, &r.page, &r.sku, &r.x, &r.y, &r.w, &r.h); err != nil {
			return err
		}
		nid, ok := im.catalogByLeg[r.cid]
		if !ok {
			continue
		}
		r.cid = nid
		r.sku = NormSKU(r.sku)
		if r.sku == "" {
			continue
		}
		k := fmt.Sprintf("%d/%d/%s/%d/%d", r.cid, r.page, r.sku, r.x, r.y)
		if seen[k] {
			continue
		}
		seen[k] = true
		buf = append(buf, r)
	}
	n, err := im.Dst.CopyFrom(ctx, pgx.Identifier{"catalog_skus"}, []string{"catalog_id", "page", "sku_norm", "x", "y", "w", "h"},
		pgx.CopyFromSlice(len(buf), func(i int) ([]any, error) {
			r := buf[i]
			return []any{r.cid, r.page, r.sku, r.x, r.y, r.w, r.h}, nil
		}))
	slog.Info("catalog skus", "rows", n)
	return err
}

func (im *Importer) covers(ctx context.Context) error {
	rows, err := im.Src.Query(ctx, `SELECT kind, key, path, catalog_id, page FROM covers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	for rows.Next() {
		var kind, key, path string
		var catLeg, page *int
		if err := rows.Scan(&kind, &key, &path, &catLeg, &page); err != nil {
			return err
		}
		var cid *int
		if catLeg != nil {
			if v, ok := im.catalogByLeg[*catLeg]; ok {
				cid = &v
			}
		}
		batch.Queue(`INSERT INTO covers (kind, key, path, catalog_id, page) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (kind, key) DO UPDATE SET path=EXCLUDED.path, catalog_id=EXCLUDED.catalog_id, page=EXCLUDED.page, updated_at=now()`, kind, key, rel(path), cid, page)
	}
	rows.Close()
	return im.Dst.SendBatch(ctx, batch).Close()
}

var ErrNoLegacy = errors.New("legacy database not reachable")
