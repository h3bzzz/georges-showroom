package store

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Family is a product detail page.
type Family struct {
	ID          int64
	Brand       Brand
	Key         string
	Title       string
	Collection  *string
	Category    *string
	Room        *string
	Tier        int16
	Description *string
	Bullets     []string
	Attributes  map[string]string
	Facts       []Fact
	Finishes    []string
	Source      string
	Items       []Item
	Media       []Media
	Documents   []Document
	Pages       []PageRef
}

// Fact is a typed attribute mined from catalogs.
type Fact struct {
	Key, Value, Unit, Source string
}

// Item is one SKU.
type Item struct {
	ID         int64
	SKU        string
	Finish     *string
	UPC        *string
	ListCents  *int64
	PriceCents *int64
	PriceFlag  *string
	LeadDays   *int
	URL        *string
	// the real photograph of this SKU in this finish, when one is keyed to it (vendor
	// master first, then a catalog photo linked to the SKU); never a recolour
	Hash *string
	ImgW *int
	ImgH *int
}

// Media is one picture in a role.
type Media struct {
	Role    string
	Rank    int16
	AssetID int64
	Hash    string
	W, H    int
	LQIP    *int
	Source  string
	How     string
	Grade   int16
	ItemID  *int64
	Finish  *string
	Catalog *int
	Page    *int
}

// Document is a spec or install sheet.
type Document struct {
	ID    int64
	Kind  string
	URL   string
	Path  *string
	Pages *int
}

// PageRef points into a catalog where the family is printed.
type PageRef struct {
	CatalogID int
	Page      int
	Title     string
}

// FamilyByID loads a product page in four small indexed queries.
func (s *Store) FamilyByID(ctx context.Context, id int64) (*Family, error) {
	f := &Family{}
	var attrs, facts []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT f.id, f.key, COALESCE(f.display_title, f.title, ''), f.collection, f.category, f.room, f.tier, f.description, f.bullets, f.attributes, f.facts, f.finishes, f.source,
		       b.id, b.code, b.slug, b.name, b.room, b.website, b.logo_path, b.featured
		FROM families f JOIN brands b ON b.id=f.brand_id WHERE f.id=$1`, id).Scan(
		&f.ID, &f.Key, &f.Title, &f.Collection, &f.Category, &f.Room, &f.Tier, &f.Description, &f.Bullets, &attrs, &facts, &f.Finishes, &f.Source,
		&f.Brand.ID, &f.Brand.Code, &f.Brand.Slug, &f.Brand.Name, &f.Brand.Room, &f.Brand.Website, &f.Brand.LogoPath, &f.Brand.Featured)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(attrs, &f.Attributes)
	var fm map[string]struct {
		Value  string `json:"value"`
		Unit   string `json:"unit"`
		Source string `json:"source"`
	}
	if json.Unmarshal(facts, &fm) == nil {
		for k, v := range fm {
			if k == "Bullets" || k == "Description" || k == "Available Finishes" || k == "Collection" {
				continue
			}
			f.Facts = append(f.Facts, Fact{k, v.Value, v.Unit, v.Source})
		}
		sort.Slice(f.Facts, func(i, j int) bool {
			return factOrder(f.Facts[i].Key) < factOrder(f.Facts[j].Key) || factOrder(f.Facts[i].Key) == factOrder(f.Facts[j].Key) && f.Facts[i].Key < f.Facts[j].Key
		})
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT i.id, i.sku, i.finish, i.upc, i.list_cents, i.price_cents, i.price_flag, i.lead_time_days, i.url, p.hash, p.width, p.height, p.source, p.url
		FROM items i
		LEFT JOIN LATERAL (
		  SELECT a.hash, a.width, a.height, a.source, a.origin->>'url' AS url
		  FROM asset_links al JOIN assets a ON a.id=al.asset_id
		  WHERE al.item_id=i.id AND NOT a.rejected AND a.built <> -1 AND a.dup_of IS NULL
		    AND (a.source='vendor' OR (a.kind='photo' AND COALESCE(a.kind_score,0) >= 0.6 AND COALESCE(a.cut,0) < 0.3))
		  ORDER BY (a.source='vendor') DESC, al.weight DESC, a.quality DESC NULLS LAST LIMIT 1
		) p ON true
		WHERE i.family_id=$1 ORDER BY (p.hash IS NOT NULL) DESC, i.price_cents NULLS LAST, i.sku`, id)
	if err != nil {
		return nil, err
	}
	codes, err := s.finishCodes(ctx, deref(f.Brand.Code))
	if err != nil {
		return nil, err
	}
	type proven struct {
		hash   string
		w, h   *int
		finish string
	}
	var proofs []proven // vendor masters whose file name names the finish they show
	f.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
		var it Item
		var src, u *string
		err := r.Scan(&it.ID, &it.SKU, &it.Finish, &it.UPC, &it.ListCents, &it.PriceCents, &it.PriceFlag, &it.LeadDays, &it.URL, &it.Hash, &it.ImgW, &it.ImgH, &src, &u)
		// a vendor master whose file name says another finish is not this SKU's photograph:
		// the finish button must say "no photograph yet" rather than show the wrong metal
		if err == nil && it.Hash != nil && src != nil && *src == "vendor" {
			if pf := picturedFinish(codes, deref(u)); pf != "" {
				proofs = append(proofs, proven{*it.Hash, it.ImgW, it.ImgH, pf})
				if it.Finish != nil && !sameFinish(pf, *it.Finish) {
					it.Hash, it.ImgW, it.ImgH = nil, nil, nil
				}
			}
		}
		return it, err
	})
	if err != nil {
		return nil, err
	}
	// a SKU without its own picture borrows a sibling's master of the same finish (the ZeroDrain
	// variant of a faucet is the same faucet in the same metal)
	for i := range f.Items {
		it := &f.Items[i]
		if it.Hash != nil || it.Finish == nil {
			continue
		}
		for _, p := range proofs {
			if sameFinish(p.finish, *it.Finish) {
				h := p.hash
				it.Hash, it.ImgW, it.ImgH = &h, p.w, p.h
				break
			}
		}
	}
	mrows, err := s.Pool.Query(ctx, `
		SELECT fm.role, fm.rank, a.id, a.hash, COALESCE(a.width,0), COALESCE(a.height,0), a.lqip, a.source, fm.how, fm.grade, fm.item_id, i.finish,
		       (a.origin->>'catalog_id')::int, (a.origin->>'page')::int, a.origin->>'url'
		FROM family_media fm JOIN assets a ON a.id=fm.asset_id LEFT JOIN items i ON i.id=fm.item_id
		WHERE fm.family_id=$1 AND a.built <> -1 AND (a.source='vendor' OR COALESCE(a.cut,0) < 0.3)
		ORDER BY CASE fm.role WHEN 'card' THEN 0 WHEN 'gallery' THEN 1 WHEN 'drawing' THEN 2 ELSE 3 END, (COALESCE(a.cut,0) >= 0.3), fm.rank`, id)
	if err != nil {
		return nil, err
	}
	f.Media, err = pgx.CollectRows(mrows, func(r pgx.CollectableRow) (Media, error) {
		var m Media
		var u *string
		err := r.Scan(&m.Role, &m.Rank, &m.AssetID, &m.Hash, &m.W, &m.H, &m.LQIP, &m.Source, &m.How, &m.Grade, &m.ItemID, &m.Finish, &m.Catalog, &m.Page, &u)
		// "Shown in X" is only claimed when the picture proves it: a vendor master named
		// with the finish code, or a catalog photo keyed to that exact SKU
		switch {
		case m.Source == "vendor":
			if pf := picturedFinish(codes, deref(u)); pf != "" {
				m.Finish = &pf
			} else {
				m.Finish = nil
			}
		case m.How != "sku" && m.How != "exact" && m.How != "vendor":
			m.Finish = nil
		}
		return m, err
	})
	if err != nil {
		return nil, err
	}
	drows, err := s.Pool.Query(ctx, `SELECT id, kind, url, path, pages FROM documents WHERE family_id=$1 ORDER BY kind, id`, id)
	if err != nil {
		return nil, err
	}
	f.Documents, err = pgx.CollectRows(drows, pgx.RowToStructByPos[Document])
	if err != nil {
		return nil, err
	}
	prows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT k.catalog_id, k.page, COALESCE(c.title, c.name)
		FROM items i JOIN catalog_skus k ON k.sku_norm=i.sku_norm JOIN catalogs c ON c.id=k.catalog_id AND c.brand_id=i.brand_id
		WHERE i.family_id=$1 ORDER BY 1, 2 LIMIT 6`, id)
	if err != nil {
		return nil, err
	}
	f.Pages, err = pgx.CollectRows(prows, pgx.RowToStructByPos[PageRef])
	return f, err
}

func factOrder(k string) int {
	switch k {
	case "Mounting", "Faucet Holes", "Handles", "Handle Style", "Function":
		return 0
	case "Spout Height", "Spout Reach", "Overall Height", "Height", "Width", "Depth", "Length", "Diameter", "Dimensions", "Centers":
		return 1
	case "Flow Rate", "Valve Type", "Cartridge", "Inlet", "Drain Included", "Drain Location", "Overflow":
		return 2
	case "Material", "Certifications", "ADA Compliant", "Designer", "Works With":
		return 3
	}
	return 4
}

// Related lists sibling families: same brand and collection first, then same category.
func (s *Store) Related(ctx context.Context, f *Family, n int) ([]Card, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT sd.family_id, sd.brand, sd.brand_slug, sd.title, sd.collection, sd.category, sd.price_cents,
		       sd.card_hash, sd.card_w, sd.card_h, sd.card_lqip, sd.grade, sd.tones, sd.finishes, sd.skus[1:4]
		FROM search_docs sd
		WHERE sd.family_id <> $1 AND sd.card_hash IS NOT NULL AND sd.tier <> 2
		  AND ((sd.brand_id = $2 AND sd.collection IS NOT DISTINCT FROM $3 AND $3 IS NOT NULL) OR (sd.category IS NOT DISTINCT FROM $4 AND $4 IS NOT NULL))
		ORDER BY (sd.brand_id = $2 AND sd.collection IS NOT DISTINCT FROM $3) DESC, (sd.brand_id = $2) DESC, sd.grade, sd.pop DESC, sd.family_id
		LIMIT $5`, f.ID, f.Brand.ID, f.Collection, f.Category, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		var c Card
		var total int
		_ = total
		if err := rows.Scan(&c.FamilyID, &c.Brand, &c.BrandSlug, &c.Title, &c.Collection, &c.Category, &c.PriceCents,
			&c.Hash, &c.W, &c.H, &c.LQIP, &c.Grade, &c.Tones, &c.Finishes, &c.SKUs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FamilyBySKU finds the family that carries a SKU (exact, normalized).
func (s *Store) FamilyBySKU(ctx context.Context, sku string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `SELECT family_id FROM items WHERE sku_norm = regexp_replace(upper($1), '[^A-Z0-9]', '', 'g') LIMIT 1`, sku).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
