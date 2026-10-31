package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"georges_/internal/site"
)

type Brand struct {
	ID       int    `json:"id"`
	Code     string `json:"code"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Logo     string `json:"logo"`
	Families int    `json:"families"`
	Products int    `json:"products"`
}

func (s *Store) UpsertBrand(ctx context.Context, code, slug, name, logo string) (int, error) {
	var id int
	var codeArg *string
	if code != "" {
		codeArg = &code
	}
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO brands (code, slug, name, logo) VALUES ($1, $2, $3, NULLIF($4,''))
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name,
		  code = COALESCE(EXCLUDED.code, brands.code), logo = COALESCE(EXCLUDED.logo, brands.logo)
		RETURNING id`, codeArg, slug, name, logo).Scan(&id)
	return id, err
}

func (s *Store) ListBrands(ctx context.Context) ([]Brand, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, COALESCE(b.code,''), b.slug, b.name, COALESCE(b.logo,''),
		  (SELECT count(*) FROM product_families f WHERE f.brand_id=b.id),
		  (SELECT count(*) FROM products p WHERE p.brand_id=b.id)
		FROM brands b ORDER BY b.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Brand])
}

func (s *Store) BrandBySlug(ctx context.Context, slug string) (*Brand, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, COALESCE(b.code,''), b.slug, b.name, COALESCE(b.logo,''),
		  (SELECT count(*) FROM product_families f WHERE f.brand_id=b.id),
		  (SELECT count(*) FROM products p WHERE p.brand_id=b.id)
		FROM brands b WHERE lower(b.slug)=lower($1)`, slug)
	if err != nil {
		return nil, err
	}
	b, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Brand])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &b, err
}

// brandSlugFromURL turns https://…/Native-trails-v1.htm into "Native-trails".
func brandSlugFromURL(u string) string {
	u = strings.TrimSuffix(u, "/")
	i := strings.LastIndex(u, "/")
	base := u[i+1:]
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".htm"), ".HTM")
	return strings.TrimSuffix(base, "-v1")
}

// familyKeyFor derives a stable family key: explicit ?model= key, else SKU minus the finish
// suffix guessed from finish options, else the SKU itself.
func familyKeyFor(p *site.Product) string {
	if p.FamilyKey != "" {
		return p.FamilyKey
	}
	if len(p.FinishOptions) > 1 {
		prefix := p.FinishOptions[0].SKU
		for _, fo := range p.FinishOptions[1:] {
			prefix = commonPrefix(prefix, fo.SKU)
		}
		prefix = strings.TrimRight(prefix, "-/. ")
		if len(prefix) >= 3 {
			return prefix
		}
	}
	return p.SKU
}

func commonPrefix(a, b string) string {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}

// UpsertProduct stores a parsed product page: brand, family, SKU, images, documents, price
// history. It is idempotent.
func (s *Store) UpsertProduct(ctx context.Context, p *site.Product) (productID int64, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	slug := brandSlugFromURL(p.BrandURL)
	if slug == "" {
		slug = strings.ReplaceAll(p.Brand, " ", "-")
	}
	var brandID int
	var brandCode string
	if err := tx.QueryRow(ctx, `
		INSERT INTO brands (slug, name) VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET name = COALESCE(NULLIF(brands.name,''), EXCLUDED.name)
		RETURNING id, COALESCE(code,'')`, slug, p.Brand).Scan(&brandID, &brandCode); err != nil {
		return 0, err
	}
	familyKey := familyKeyFor(p)
	if brandCode != "" {
		familyKey = strings.TrimPrefix(familyKey, strings.ToUpper(brandCode)+"-")
	}

	var cats []string
	for i, c := range p.Breadcrumbs {
		if i > 0 {
			cats = append(cats, c.Text)
		}
	}
	attrs, _ := json.Marshal(p.Attributes)
	room := RoomFor(cats, p.ProductType, p.Name, p.Brand)
	var familyID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO product_families (brand_id, family_key, title, description, product_type, collection, category_path, attributes, bullets, search, room)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7::text[],'{}'),$8,COALESCE($9::text[],'{}'),
		  to_tsvector('english', coalesce($3,'')||' '||coalesce($4,'')||' '||coalesce($5,'')||' '||coalesce($6,'')||' '||$2), NULLIF($10,''))
		ON CONFLICT (brand_id, family_key) DO UPDATE SET
		  room = COALESCE(EXCLUDED.room, product_families.room),
		  title = COALESCE(EXCLUDED.title, product_families.title),
		  description = COALESCE(NULLIF(EXCLUDED.description,''), product_families.description),
		  product_type = COALESCE(NULLIF(EXCLUDED.product_type,''), product_families.product_type),
		  collection = COALESCE(NULLIF(EXCLUDED.collection,''), product_families.collection),
		  category_path = CASE WHEN cardinality(EXCLUDED.category_path)>0 THEN EXCLUDED.category_path ELSE product_families.category_path END,
		  attributes = product_families.attributes || EXCLUDED.attributes,
		  bullets = CASE WHEN cardinality(EXCLUDED.bullets)>0 THEN EXCLUDED.bullets ELSE product_families.bullets END,
		  search = EXCLUDED.search, updated_at = now()
		RETURNING id`,
		brandID, familyKey, p.Name, p.Description, p.ProductType, p.Collection, cats, attrs, p.Bullets, room).Scan(&familyID); err != nil {
		return 0, err
	}

	var opid *int64
	if p.OnlineProductID != 0 {
		v := p.OnlineProductID
		opid = &v
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO products (brand_id, family_id, sku, upc, finish, online_product_id, url, name, price_cents, msrp_cents, currency, quote_id)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,NULLIF($9,0),NULLIF($10,0),$11,NULLIF($12,''))
		ON CONFLICT (brand_id, sku) DO UPDATE SET
		  family_id = EXCLUDED.family_id, upc = COALESCE(EXCLUDED.upc, products.upc),
		  finish = COALESCE(EXCLUDED.finish, products.finish),
		  online_product_id = COALESCE(EXCLUDED.online_product_id, products.online_product_id),
		  url = EXCLUDED.url, name = EXCLUDED.name,
		  price_cents = COALESCE(EXCLUDED.price_cents, products.price_cents),
		  msrp_cents = COALESCE(EXCLUDED.msrp_cents, products.msrp_cents),
		  quote_id = COALESCE(EXCLUDED.quote_id, products.quote_id), last_seen = now()
		RETURNING id`,
		brandID, familyID, p.SKU, p.UPC, p.Finish, opid, p.URL, p.Name, p.PriceCents, p.MSRPCents, p.Currency, p.QuoteID).Scan(&productID); err != nil {
		return 0, err
	}
	if p.PriceCents > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO price_history (product_id, observed, price_cents, msrp_cents) VALUES ($1, CURRENT_DATE, $2, NULLIF($3,0))
			ON CONFLICT (product_id, observed) DO UPDATE SET price_cents = EXCLUDED.price_cents, msrp_cents = EXCLUDED.msrp_cents`,
			productID, p.PriceCents, p.MSRPCents); err != nil {
			return 0, err
		}
	}
	for i, img := range p.Images {
		local := p.LocalImages[img]
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_images (product_id, family_id, source, url, position, path) VALUES ($1,$2,'site',$3,$4,NULLIF($5,''))
			ON CONFLICT (source, url) DO UPDATE SET product_id = EXCLUDED.product_id, family_id = EXCLUDED.family_id, position = EXCLUDED.position,
			  path = COALESCE(EXCLUDED.path, product_images.path)`,
			productID, familyID, img, i, local); err != nil {
			return 0, err
		}
	}
	for _, d := range p.Documents {
		if _, err := tx.Exec(ctx, `
			INSERT INTO documents (family_id, product_id, kind, url) VALUES ($1,$2,$3,$4)
			ON CONFLICT (url) DO UPDATE SET family_id = EXCLUDED.family_id, product_id = COALESCE(documents.product_id, EXCLUDED.product_id)`,
			familyID, productID, d.Kind, d.URL); err != nil {
			return 0, err
		}
	}
	// sibling finishes we know about but have not crawled yet (price + finish + url)
	for _, fo := range p.FinishOptions {
		if fo.SKU == "" || fo.SKU == p.SKU {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO products (brand_id, family_id, sku, finish, url, name, price_cents, currency)
			VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,NULLIF($7,0),$8)
			ON CONFLICT (brand_id, sku) DO UPDATE SET family_id = COALESCE(products.family_id, EXCLUDED.family_id),
			  finish = COALESCE(products.finish, EXCLUDED.finish), url = COALESCE(products.url, EXCLUDED.url),
			  price_cents = COALESCE(products.price_cents, EXCLUDED.price_cents)`,
			brandID, familyID, fo.SKU, fo.Finish, fo.URL, p.Name, fo.PriceCents, p.Currency); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crawl_state (url, kind, status, last_fetch) VALUES ($1,'product',200,now())
		ON CONFLICT (url) DO UPDATE SET status=200, last_fetch=now(), error=NULL`, p.URL); err != nil {
		return 0, err
	}
	return productID, tx.Commit(ctx)
}

// ---------- read side for the web app ----------

type FamilyRow struct {
	ID          int64             `json:"id"`
	BrandID     int               `json:"brand_id"`
	BrandName   string            `json:"brand_name"`
	BrandSlug   string            `json:"brand_slug"`
	FamilyKey   string            `json:"family_key"`
	Title       string            `json:"title"`
	ProductType string            `json:"product_type"`
	Collection  string            `json:"collection"`
	Category    []string          `json:"category_path"`
	Attributes  map[string]string `json:"attributes"`
	Bullets     []string          `json:"bullets"`
	Description string            `json:"description"`
	Image       string            `json:"image"`
	MinCents    int64             `json:"min_cents"`
	MaxCents    int64             `json:"max_cents"`
	Finishes    int               `json:"finishes"`
	ListMin     int64             `json:"list_min_cents"`
	ListMax     int64             `json:"list_max_cents"`
	ImageW      int               `json:"image_w"`      // source width of Image (0 = unknown); small crops are not blown up
	ImageFinish string            `json:"image_finish"` // finish of the SKU the shown crop belongs to ("" = unknown)
}

const familySelect = `
	SELECT f.id, f.brand_id, b.name, b.slug, f.family_key, COALESCE(f.title,''), COALESCE(f.product_type,''),
	  COALESCE(f.collection,''), f.category_path, f.attributes, f.bullets, COALESCE(f.description,''),
	  COALESCE('/files/'||fb.web_path, '/files/'||fb.photo_path, ''), -- a vendor master (2000px, white) beats any catalog crop
	  COALESCE((SELECT min(our_cents(p.source, p.price_cents, p.msrp_cents, b.price_multiplier)) FROM products p WHERE p.family_id=f.id),0),
	  COALESCE((SELECT max(our_cents(p.source, p.price_cents, p.msrp_cents, b.price_multiplier)) FROM products p WHERE p.family_id=f.id),0),
	  (SELECT count(*) FROM products p WHERE p.family_id=f.id)::int,
	  COALESCE((SELECT min(list_cents(p.source, p.price_cents, p.msrp_cents)) FROM products p WHERE p.family_id=f.id),0),
	  COALESCE((SELECT max(list_cents(p.source, p.price_cents, p.msrp_cents)) FROM products p WHERE p.family_id=f.id),0),
	  CASE WHEN fb.web_path IS NOT NULL THEN 0 ELSE COALESCE(fb.photo_w, 0) END,
	  COALESCE((SELECT p.finish FROM catalog_crops cc JOIN products p ON p.family_id=f.id AND (p.sku=cc.sku OR (cc.base_n IS NOT NULL AND p.sku_n=cc.base_n))
	            WHERE fb.photo_path IS NOT NULL AND (cc.path=fb.photo_path OR cc.hd_path=fb.photo_path) AND COALESCE(p.finish,'')<>'' LIMIT 1),'')
	FROM product_families f JOIN brands b ON b.id=f.brand_id LEFT JOIN family_best fb ON fb.family_id=f.id`

type FamilyFilter struct {
	BrandSlug string
	Category  string
	Query     string
	Page      int
	PerPage   int
}

func (s *Store) ListFamilies(ctx context.Context, f FamilyFilter) ([]FamilyRow, int, error) {
	if f.PerPage <= 0 {
		f.PerPage = 24
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	where, args := []string{"TRUE"}, []any{}
	if f.BrandSlug != "" {
		args = append(args, f.BrandSlug)
		where = append(where, "lower(b.slug)=lower($"+itoa(len(args))+")")
	}
	if f.Category != "" {
		args = append(args, f.Category)
		where = append(where, "$"+itoa(len(args))+" = ANY(f.category_path)")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, ParseIntent(q, s.brandNames(ctx)).conds(&args, false)...)
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM product_families f JOIN brands b ON b.id=f.brand_id LEFT JOIN family_best fb ON fb.family_id=f.id"+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	rows, err := s.Pool.Query(ctx, familySelect+w+" ORDER BY f.tier, (fb.photo_path IS NULL), b.name, f.title LIMIT $"+itoa(len(args)-1)+" OFFSET $"+itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[FamilyRow])
	return out, total, err
}

func (s *Store) FamilyByID(ctx context.Context, id int64) (*FamilyRow, error) {
	rows, err := s.Pool.Query(ctx, familySelect+" WHERE f.id=$1", id)
	if err != nil {
		return nil, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[FamilyRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

type ProductRow struct {
	ID         int64  `json:"id"`
	SKU        string `json:"sku"`
	UPC        string `json:"upc"`
	Finish     string `json:"finish"`
	URL        string `json:"url"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	MSRPCents  int64  `json:"msrp_cents"`
	Image      string `json:"image"`
}

func (s *Store) ProductsForFamily(ctx context.Context, familyID int64) ([]ProductRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.sku, COALESCE(p.upc,''), COALESCE(p.finish,''), COALESCE(p.url,''), COALESCE(p.name,''),
		  COALESCE(our_cents(p.source, p.price_cents, p.msrp_cents, (SELECT price_multiplier FROM brands b WHERE b.id=p.brand_id)),0),
		  COALESCE(list_cents(p.source, p.price_cents, p.msrp_cents),0),
		  COALESCE((SELECT '/files/'||COALESCE(cc.hd_path,cc.path) FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id
		     WHERE fc.family_id=p.family_id AND cc.kind='photo' ORDER BY (cc.sku=p.sku) DESC, cc.score DESC, cc.w*cc.h DESC LIMIT 1),'')
		FROM products p WHERE p.family_id=$1 ORDER BY p.finish, p.sku`, familyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ProductRow])
}

type DocRow struct {
	ID   int64  `json:"id"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
	Path string `json:"path"`
}

func (s *Store) DocumentsForFamily(ctx context.Context, familyID int64) ([]DocRow, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, kind, url, COALESCE(path,'') FROM documents WHERE family_id=$1 ORDER BY kind, id`, familyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DocRow])
}

func (s *Store) ImagesForFamily(ctx context.Context, familyID int64) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT u FROM (
		  SELECT '/files/'||COALESCE(cc.hd_path,cc.path) AS u, 0 AS pri, (fc.how='exact') AS ex, cc.score AS sc, cc.w*cc.h AS ar
		  FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id WHERE fc.family_id=$1 AND cc.kind='photo' AND cc.dup_of IS NULL
		  UNION ALL
		  SELECT '/files/'||i.path, 1, true, 1, 0 FROM product_images i WHERE i.family_id=$1 AND i.source='web' AND i.path IS NOT NULL
		  UNION ALL
		  SELECT '/files/'||i.path, 2, true, 1, i.width*i.height FROM product_images i
		  WHERE i.family_id=$1 AND i.source='spec' AND i.path IS NOT NULL AND i.width >= 400 AND i.height >= 300
		) x ORDER BY pri, ex DESC, sc DESC, ar DESC LIMIT 12`, familyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

type CategoryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (s *Store) TopCategories(ctx context.Context, brandSlug string, limit int) ([]CategoryCount, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c, count(*)::int FROM product_families f JOIN brands b ON b.id=f.brand_id, unnest(f.category_path) c
		WHERE ($1='' OR lower(b.slug)=lower($1)) AND c NOT IN ('None','') GROUP BY c ORDER BY 2 DESC LIMIT $2`, brandSlug, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CategoryCount])
}

// ---------- catalogs ----------

type CatalogRow struct {
	ID        int       `json:"id"`
	BrandCode string    `json:"brand_code"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	Pages     int       `json:"pages"`
	HasText   bool      `json:"has_text"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (s *Store) UpsertCatalog(ctx context.Context, code, name, title string, pages int, createdRaw string, hasText bool, fetched bool) (int, error) {
	var id int
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO catalogs (brand_code, name, title, pages, created_raw, has_text, fetched_at)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6, CASE WHEN $7 THEN now() END)
		ON CONFLICT (brand_code, name) DO UPDATE SET title=COALESCE(EXCLUDED.title, catalogs.title), pages=EXCLUDED.pages,
		  created_raw=EXCLUDED.created_raw, has_text=EXCLUDED.has_text,
		  fetched_at=COALESCE(EXCLUDED.fetched_at, catalogs.fetched_at)
		RETURNING id`, code, name, title, pages, createdRaw, hasText, fetched).Scan(&id)
	return id, err
}

func (s *Store) UpsertCatalogPage(ctx context.Context, catalogID, page int, path, text string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO catalog_pages (catalog_id, page, path, text) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''))
		ON CONFLICT (catalog_id, page) DO UPDATE SET path=COALESCE(EXCLUDED.path, catalog_pages.path), text=COALESCE(EXCLUDED.text, catalog_pages.text)`,
		catalogID, page, path, text)
	return err
}

func (s *Store) ListCatalogs(ctx context.Context, brandCode string) ([]CatalogRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, brand_code, name, COALESCE(title,''), COALESCE(pages,0), COALESCE(has_text,false), COALESCE(fetched_at, 'epoch'::timestamptz)
		FROM catalogs WHERE ($1='' OR brand_code=$1) ORDER BY brand_code, name`, brandCode)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogRow])
}

func (s *Store) CatalogByID(ctx context.Context, id int) (*CatalogRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, brand_code, name, COALESCE(title,''), COALESCE(pages,0), COALESCE(has_text,false), COALESCE(fetched_at, 'epoch'::timestamptz)
		FROM catalogs WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[CatalogRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func (s *Store) CatalogPagePath(ctx context.Context, id, page int) (string, error) {
	var p string
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE(path,'') FROM catalog_pages WHERE catalog_id=$1 AND page=$2`, id, page).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return p, err
}

type CatalogHit struct {
	CatalogID int    `json:"catalog_id"`
	BrandCode string `json:"brand_code"`
	Title     string `json:"title"`
	Page      int    `json:"page"`
	Snippet   string `json:"snippet"`
}

func (s *Store) SearchCatalogText(ctx context.Context, q string, limit int) ([]CatalogHit, error) {
	// match via the indexed tsvector (catalog_pages.search) or a trigram substring on the text
	// layer; the headline is computed only for the LIMITed rows.
	rows, err := s.Pool.Query(ctx, `
		WITH hits AS (
		  SELECT c.id, c.brand_code, COALESCE(c.title,c.name) AS title, p.page, COALESCE(p.text,p.ocr_text,'') AS body
		  FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id
		  WHERE p.search @@ websearch_to_tsquery('english',$1)
		     OR p.text ILIKE '%'||$1||'%'
		  ORDER BY c.brand_code, p.page LIMIT $2)
		SELECT id, brand_code, title, page,
		  ts_headline('english', body, websearch_to_tsquery('english',$1), 'MaxWords=25, MinWords=10')
		FROM hits ORDER BY brand_code, page`, q, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogHit])
}

func (s *Store) Counts(ctx context.Context) (brands, families, products, catalogs, pages int, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM brands),(SELECT count(*) FROM product_families),(SELECT count(*) FROM products),(SELECT count(*) FROM catalogs),(SELECT count(*) FROM catalog_pages)`).
		Scan(&brands, &families, &products, &catalogs, &pages)
	return
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}

// PendingImages lists site image URLs that have no local copy yet.
func (s *Store) PendingImages(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT url FROM product_images WHERE source='site' AND (path IS NULL OR path='') AND url<>'' ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) SetImagePath(ctx context.Context, url, path string, w, h int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE product_images SET path=$2, width=NULLIF($3,0), height=NULLIF($4,0) WHERE url=$1`, url, path, w, h)
	return err
}

// PendingLogos returns brands whose logo still points at the old site.
func (s *Store) PendingLogos(ctx context.Context) ([]Brand, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, COALESCE(code,''), slug, name, COALESCE(logo,''), 0, 0 FROM brands WHERE logo LIKE 'https://www.georgesshowroom.com/%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Brand])
}

func (s *Store) SetBrandLogo(ctx context.Context, id int, logo string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE brands SET logo=$2 WHERE id=$1`, id, logo)
	return err
}

// CatalogByBook finds a catalog by "CODE/name".
func (s *Store) CatalogByBook(ctx context.Context, code, name string) (*CatalogRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, brand_code, name, COALESCE(title,''), COALESCE(pages,0), COALESCE(has_text,false), COALESCE(fetched_at, 'epoch'::timestamptz)
		FROM catalogs WHERE brand_code=$1 AND name=$2`, code, name)
	if err != nil {
		return nil, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[CatalogRow])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// NewestCatalogs returns the most recently published books (by the flipbook's CreatedTime).
func (s *Store) NewestCatalogs(ctx context.Context, limit int) ([]CatalogRow, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, brand_code, name, COALESCE(title,''), COALESCE(pages,0), COALESCE(has_text,false), COALESCE(fetched_at, 'epoch'::timestamptz)
		FROM catalogs WHERE COALESCE(pages,0) > 8 ORDER BY created_raw DESC NULLS LAST, id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogRow])
}

// CropsWithCatalog joins crops to their catalog for display.
type CropView struct {
	CropRow
	BrandCode string
	Title     string
}

func (s *Store) CropsForSKUView(ctx context.Context, sku string) ([]CropView, error) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	rows, err := s.Pool.Query(ctx, `
		SELECT cc.id, cc.catalog_id, cc.page, cc.x, cc.y, cc.w, cc.h, cc.path, COALESCE(cc.sku,''), COALESCE(cc.score,0), COALESCE(cc.kind,'photo'), c.brand_code, COALESCE(c.title,c.name)
		FROM catalog_crops cc JOIN catalogs c ON c.id=cc.catalog_id
		WHERE cc.sku=$1 OR cc.sku=split_part($1,'-',1) OR ($1 <> '' AND cc.sku LIKE $1||'%')
		ORDER BY (cc.sku=$1) DESC, (cc.kind='photo') DESC, cc.score DESC, cc.w*cc.h DESC LIMIT 24`, sku)
	if err != nil {
		return nil, err
	}
	var out []CropView
	for rows.Next() {
		var v CropView
		if err := rows.Scan(&v.ID, &v.CatalogID, &v.Page, &v.X, &v.Y, &v.W, &v.H, &v.Path, &v.SKU, &v.Score, &v.Kind, &v.BrandCode, &v.Title); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PagesMentioning lists catalog pages whose OCR/text layer contains the SKU.
func (s *Store) PagesMentioning(ctx context.Context, sku string, limit int) ([]CatalogHit, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, COALESCE(c.title,c.name), p.page,
		  ts_headline('english', COALESCE(p.ocr_text,p.text,''), plainto_tsquery('english',$1), 'MaxWords=20, MinWords=8')
		FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id
		WHERE COALESCE(p.ocr_text,'') ILIKE '%'||$1||'%' OR COALESCE(p.text,'') ILIKE '%'||$1||'%'
		ORDER BY c.brand_code, p.page LIMIT $2`, sku, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogHit])
}

func (s *Store) SetBrandFeatured(ctx context.Context, slug string, featured bool, domain string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE brands SET featured=$2, domain=COALESCE(NULLIF($3,''), domain) WHERE lower(slug)=lower($1)`, slug, featured, domain)
	return err
}

func (s *Store) BrandSlugByCode(ctx context.Context, code string) (string, error) {
	var slug string
	err := s.Pool.QueryRow(ctx, `SELECT slug FROM brands WHERE upper(code)=upper($1)`, code).Scan(&slug)
	return slug, err
}

func (s *Store) SetBrandLogoBySlug(ctx context.Context, slug, logo string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE brands SET logo=$2 WHERE lower(slug)=lower($1)`, slug, logo)
	return err
}

// BrandCard is a brand with everything the brand wall / brands page shows.
type BrandCard struct {
	Brand
	Featured bool
	Domain   string
	Catalogs int
	Rooms    []string
	Cover    int // a catalog id whose cover can illustrate the brand
}

func (s *Store) BrandCards(ctx context.Context, featuredOnly bool) ([]BrandCard, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT b.id, COALESCE(b.code,''), b.slug, b.name, COALESCE(b.logo,''),
		  (SELECT count(*) FROM product_families f WHERE f.brand_id=b.id)::int,
		  (SELECT count(*) FROM products p WHERE p.brand_id=b.id)::int,
		  b.featured, COALESCE(b.domain,''),
		  (SELECT count(*) FROM catalogs c WHERE c.brand_code=b.code)::int,
		  COALESCE((SELECT array_agg(DISTINCT room ORDER BY room) FROM product_families f WHERE f.brand_id=b.id AND room IS NOT NULL), '{}'),
		  COALESCE((SELECT c.id FROM catalogs c WHERE c.brand_code=b.code AND COALESCE(c.pages,0)>8 ORDER BY c.created_raw DESC NULLS LAST LIMIT 1),0)
		FROM brands b WHERE NOT $1 OR b.featured ORDER BY b.name`, featuredOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrandCard
	for rows.Next() {
		var c BrandCard
		if err := rows.Scan(&c.ID, &c.Code, &c.Slug, &c.Name, &c.Logo, &c.Families, &c.Products, &c.Featured, &c.Domain, &c.Catalogs, &c.Rooms, &c.Cover); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PagesForFamily finds catalog pages (same brand) whose SKU tokens match the family's SKUs or key.
func (s *Store) PagesForFamily(ctx context.Context, familyID int64, limit int) ([]CatalogHit, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT ON (c.id, k.page) c.id, c.brand_code, COALESCE(c.title,c.name), k.page, ''
		FROM product_families f JOIN brands b ON b.id=f.brand_id
		JOIN catalogs c ON c.brand_code=b.code
		JOIN catalog_skus k ON k.catalog_id=c.id
		WHERE f.id=$1 AND k.sku_base IS NOT NULL AND length(k.sku_base) >= 4
		  AND (k.sku_base = f.family_key OR f.family_key LIKE k.sku_base||'%' OR f.key_n = k.base_n
		       OR EXISTS (SELECT 1 FROM products p WHERE p.family_id=f.id AND (p.sku=k.sku OR p.sku LIKE k.sku_base||'%' OR p.sku_n LIKE k.base_n||'%')))
		ORDER BY c.id, k.page, c.created_raw DESC NULLS LAST LIMIT $2`, familyID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CatalogHit])
}

type WebImageTask struct {
	FamilyID  int64
	BrandSlug string
	Domain    string
	FamilyKey string
	SKU       string
}

// FamiliesNeedingWebImage lists families with a brand domain, no catalog crop link and no web
// image, oldest-untried first.
func (s *Store) FamiliesNeedingWebImage(ctx context.Context, brandSlug string, limit int) ([]WebImageTask, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT f.id, b.slug, b.domain, f.family_key,
		  COALESCE((SELECT p.sku FROM products p WHERE p.family_id=f.id ORDER BY (p.price_cents IS NULL), p.sku LIMIT 1), f.family_key)
		FROM product_families f JOIN brands b ON b.id=f.brand_id
		WHERE COALESCE(b.domain,'')<>'' AND ($1='' OR lower(b.slug)=lower($1))
		  AND NOT EXISTS (SELECT 1 FROM family_crops fc WHERE fc.family_id=f.id)
		  AND NOT EXISTS (SELECT 1 FROM product_images i WHERE i.family_id=f.id AND i.source='web')
		  AND (f.webimg_tried_at IS NULL OR f.webimg_tried_at < now() - interval '30 days')
		ORDER BY f.webimg_tried_at NULLS FIRST, f.id LIMIT $2`, brandSlug, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[WebImageTask])
}

func (s *Store) MarkWebImageTried(ctx context.Context, familyID int64, found bool) error {
	_, err := s.Pool.Exec(ctx, `UPDATE product_families SET webimg_tried_at=now() WHERE id=$1`, familyID)
	return err
}

func (s *Store) AddWebImage(ctx context.Context, familyID int64, url, path, pageURL string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO product_images (family_id, source, url, path, page_url, position) VALUES ($1,'web',$2,$3,$4,0)
		ON CONFLICT (source, url) DO UPDATE SET family_id=EXCLUDED.family_id, path=EXCLUDED.path, page_url=EXCLUDED.page_url`, familyID, url, path, pageURL)
	return err
}

// LifestylePage picks the page of a catalog with the largest photo and little text — a
// spread that works as a wide banner. Returns 0 when the book has no such page yet.
func (s *Store) LifestylePage(ctx context.Context, catalogID int) int {
	var page int
	_ = s.Pool.QueryRow(ctx, `
		SELECT cc.page FROM catalog_crops cc JOIN catalog_pages p ON p.catalog_id=cc.catalog_id AND p.page=cc.page
		WHERE cc.catalog_id=$1 AND cc.kind='photo' AND length(COALESCE(p.ocr_text,'')) < 250 AND cc.page > 1
		  AND cc.w*cc.h > 700000
		ORDER BY cc.w*cc.h DESC LIMIT 1`, catalogID).Scan(&page)
	return page
}

// DrawingsForFamily returns technical drawings linked to the family (shown apart from photos).
func (s *Store) DrawingsForFamily(ctx context.Context, familyID int64) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT '/files/'||COALESCE(cc.hd_path,cc.path) FROM family_crops fc JOIN catalog_crops cc ON cc.id=fc.crop_id
		WHERE fc.family_id=$1 AND cc.kind='drawing' AND cc.dup_of IS NULL ORDER BY (fc.how='exact') DESC, cc.w*cc.h DESC LIMIT 6`, familyID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Cover is a lifestyle image cut from a catalog page.
type Cover struct {
	Path      string
	CatalogID int
	Page      int
	W, H      int
}

// LifestyleCrop returns the largest wide photo on a low-text page of one catalog: a spread
// photograph without the page furniture around it.
func (s *Store) LifestyleCrop(ctx context.Context, catalogID int) (Cover, error) {
	var c Cover
	err := s.Pool.QueryRow(ctx, `
		SELECT cc.path, cc.catalog_id, cc.page, cc.w, cc.h
		FROM catalog_crops cc JOIN catalog_pages p ON p.catalog_id=cc.catalog_id AND p.page=cc.page
		WHERE cc.catalog_id=$1 AND cc.kind='photo' AND length(COALESCE(p.ocr_text,'')) < 300 AND cc.w >= 900 AND cc.w >= cc.h*0.8
		ORDER BY cc.w*cc.h DESC LIMIT 1`, catalogID).Scan(&c.Path, &c.CatalogID, &c.Page, &c.W, &c.H)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cover{}, nil
	}
	return c, err
}

// CategoryTile is a room-page tile: a category name, its family count and a lifestyle cover.
type CategoryTile struct {
	Name  string
	Count int
	Cover Cover
}

// CategoryTiles returns the merchandised categories of a room that have products, each with
// a cover: a curated pick, else a lifestyle photograph from a catalog page about the
// category (never the same photo twice on one page), else the best product photo.
func (s *Store) CategoryTiles(ctx context.Context, room string, limit int) ([]CategoryTile, error) {
	used := map[string]bool{}
	var out []CategoryTile
	for _, rc := range RoomCategories[room] {
		if len(out) >= limit {
			break
		}
		var n int
		if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM product_families f WHERE f.room=$1 AND (f.category_path && $3 OR (COALESCE(array_length(f.category_path,1),0)=0 AND `+categoryMatch("$2")+`))`, room, rc.Keyword, categoriesForFilter(rc.Name)).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		t := CategoryTile{Name: rc.Name, Count: n}
		if cur, _ := s.GetCover(ctx, "category", CoverKey(room, rc.Name)); cur.Path != "" {
			t.Cover = cur
		} else {
			cands, _ := s.CoverCandidates(ctx, room, rc.Photo, 8)
			for _, c := range cands {
				if !used[c.Path] {
					t.Cover = c
					break
				}
			}
			if t.Cover.Path == "" {
				_ = s.Pool.QueryRow(ctx, `
					SELECT cc.path, cc.catalog_id, cc.page, cc.w, cc.h
					FROM product_families f JOIN family_crops fc ON fc.family_id=f.id JOIN catalog_crops cc ON cc.id=fc.crop_id
					WHERE f.room=$1 AND brand_room(f.brand_id)=$1 AND cc.kind='photo' AND `+categoryMatch("$2")+`
					ORDER BY cc.w*cc.h DESC LIMIT 1`, room, rc.Keyword).Scan(&t.Cover.Path, &t.Cover.CatalogID, &t.Cover.Page, &t.Cover.W, &t.Cover.H)
			}
		}
		if t.Cover.Path != "" {
			used[t.Cover.Path] = true
		}
		out = append(out, t)
	}
	return out, nil
}

// categoryMatch is the SQL predicate for "family belongs to this merchandised category".
func categoryMatch(param string) string {
	return "(EXISTS (SELECT 1 FROM unnest(f.category_path) c WHERE c ILIKE '%'||" + param + "||'%') OR COALESCE(f.product_type,'') ILIKE '%'||" + param + "||'%' OR COALESCE(f.title,'') ILIKE '%'||" + param + "||'%')"
}

// TopCategoriesInRoom counts families per category inside one room.
func (s *Store) TopCategoriesInRoom(ctx context.Context, room string, limit int) ([]CategoryCount, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c, count(*)::int FROM product_families f, unnest(f.category_path) c
		WHERE f.room=$1 AND c NOT IN ('None','') GROUP BY c ORDER BY 2 DESC LIMIT $2`, room, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CategoryCount])
}

// keywordFor reduces a category name to the word a catalog page would use ("Shower Heads" →
// "shower", "Bathroom Vanities" → "vanit").
func keywordFor(cat string) string {
	l := strings.ToLower(cat)
	for _, k := range []string{"shower", "tub", "toilet", "vanit", "mirror", "sink", "faucet", "knob", "pull", "lever", "hinge", "handle", "range", "oven", "refrigerat", "dishwasher", "hood", "steam", "drain", "bidet", "cabinet", "lighting", "accessor"} {
		if strings.Contains(l, k) {
			return k
		}
	}
	w := strings.Fields(l)
	if len(w) == 0 {
		return l
	}
	return strings.TrimSuffix(w[len(w)-1], "s")
}

// BrandBook is one representative catalog per brand for the home-page wheel.
type BrandBook struct {
	CatalogID int
	BrandCode string
	BrandName string
	BrandSlug string
	Title     string
	Pages     int
}

// BrandBooks picks the best-looking book per brand: a catalog/collection/lookbook before a
// price list, then the newest and the thickest. Ordered by brand name.
func (s *Store) BrandBooks(ctx context.Context) ([]BrandBook, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.brand_code, b.name, b.slug, COALESCE(NULLIF(c.title,''), c.name), COALESCE(c.pages,0)
		FROM (
		  SELECT DISTINCT ON (c.brand_code) c.*
		  FROM catalogs c
		  WHERE COALESCE(c.pages,0) >= 6
		  ORDER BY c.brand_code,
		    (COALESCE(c.title,c.name) ~* '(catalog|collection|brochure|lookbook|design guide|magazine)') DESC,
		    (COALESCE(c.title,c.name) ~* '(price|pricing|addendum|fees|terms)') ASC,
		    c.created_raw DESC NULLS LAST, c.pages DESC
		) c JOIN brands b ON b.code=c.brand_code
		ORDER BY b.name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[BrandBook])
}

// AddProductWebImage records a manufacturer master for one SKU (and its family).
func (s *Store) AddProductWebImage(ctx context.Context, familyID, productID int64, url, path string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO product_images (family_id, product_id, source, url, path, page_url, position) VALUES ($1,$2,'web',$3,$4,$3,0)
		ON CONFLICT (source, url) DO UPDATE SET family_id=EXCLUDED.family_id, product_id=EXCLUDED.product_id, path=EXCLUDED.path`,
		familyID, productID, url, path)
	return err
}

// MarkProductImageMiss remembers that the vendor CDN has no master for this SKU, so the next
// run does not ask again (a row with no path).
func (s *Store) MarkProductImageMiss(ctx context.Context, productID int64) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO product_images (product_id, source, url, position) VALUES ($1::bigint,'web','miss:'||$1::text,0)
		ON CONFLICT (source, url) DO NOTHING`, productID)
	return err
}
