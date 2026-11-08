package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Loader upserts feed files into feed_products.
type Loader struct {
	Pool *pgxpool.Pool
	Dir  string // folder holding the mirrored xml files
	// Brand limits the run to one brand code.
	Brand string
}

var reFeedFile = regexp.MustCompile(`^([A-Z0-9]+)(-EXT)?-(\d{4}-\d{2}-\d{2})\.xml$`)

// Files groups the newest main and EXT file per brand.
type Files struct {
	Brand, Main, Ext string
	Date             string
}

// Discover lists the newest feed per brand in dir.
func Discover(dir string) ([]Files, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	by := map[string]*Files{}
	for _, e := range ents {
		m := reFeedFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		f := by[m[1]]
		if f == nil {
			f = &Files{Brand: m[1]}
			by[m[1]] = f
		}
		p := filepath.Join(dir, e.Name())
		if m[2] == "" {
			if f.Main == "" || m[3] > f.Date {
				f.Main, f.Date = p, m[3]
			}
		} else if f.Ext == "" || e.Name() > filepath.Base(f.Ext) {
			f.Ext = p
		}
	}
	var out []Files
	for _, f := range by {
		if f.Main != "" {
			out = append(out, *f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Brand < out[j].Brand })
	return out, nil
}

// Run loads every brand (or one). Returns products upserted.
func (l *Loader) Run(ctx context.Context) (int, error) {
	files, err := Discover(l.Dir)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, f := range files {
		if l.Brand != "" && f.Brand != l.Brand {
			continue
		}
		t := time.Now()
		n, err := l.loadBrand(ctx, f)
		if err != nil {
			return total, fmt.Errorf("%s: %w", f.Brand, err)
		}
		total += n
		slog.Info("feed loaded", "brand", f.Brand, "products", n, "date", f.Date, "took", time.Since(t).Round(time.Millisecond))
	}
	return total, nil
}

func (l *Loader) loadBrand(ctx context.Context, f Files) (int, error) {
	// EXT attributes first, keyed by SKU, so the main pass stores them with the row
	ext := map[string]map[string]string{}
	if f.Ext != "" {
		err := ParseFile(f.Ext, func(r Record) error {
			sku := NormSKU(r["sku"])
			if sku == "" {
				return nil
			}
			a := ext[sku]
			if a == nil {
				a = map[string]string{}
				ext[sku] = a
			}
			for k, v := range r.Attrs() {
				a[k] = v
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	batch := &pgx.Batch{}
	n := 0
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		br := l.Pool.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return err
			}
		}
		batch = &pgx.Batch{}
		return br.Close()
	}
	file := filepath.Base(f.Main)
	err := ParseFile(f.Main, func(r Record) error {
		pid, _ := strconv.ParseInt(r["productID"], 10, 64)
		sku := r["sku"]
		if pid == 0 || sku == "" {
			return nil
		}
		attrs := r.Attrs()
		for k, v := range ext[NormSKU(sku)] {
			attrs[k] = v
		}
		aj, _ := json.Marshal(attrs)
		imgType, _ := strconv.Atoi(r["imageType"])
		batch.Queue(`
			INSERT INTO feed_products (product_id, brand_code, brand_name, sku, sku_norm, alt_sku, model, upc, cx1_category_id, type, grp, category, sub_category,
			  m_category, m_sub_category, collection, product_name, finish, finish_group, msrp_cents, map_cents, dn_cents, price_group, currency,
			  image_name, image_path, image_type, alt_images, spec_link, install_link, web_link, description, marketing, bullets, attrs, created, updated, file, loaded_at)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),
			  NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),NULLIF($19,''),NULLIF($20,0),NULLIF($21,0),NULLIF($22,0),NULLIF($23,''),NULLIF($24,''),
			  NULLIF($25,''),NULLIF($26,''),$27,$28,NULLIF($29,''),NULLIF($30,''),NULLIF($31,''),NULLIF($32,''),NULLIF($33,''),$34,$35,NULLIF($36,'')::date,NULLIF($37,'')::date,$38,now())
			ON CONFLICT (product_id) DO UPDATE SET brand_code=EXCLUDED.brand_code, brand_name=EXCLUDED.brand_name, sku=EXCLUDED.sku, sku_norm=EXCLUDED.sku_norm,
			  alt_sku=EXCLUDED.alt_sku, model=EXCLUDED.model, upc=EXCLUDED.upc, cx1_category_id=EXCLUDED.cx1_category_id, type=EXCLUDED.type, grp=EXCLUDED.grp,
			  category=EXCLUDED.category, sub_category=EXCLUDED.sub_category, m_category=EXCLUDED.m_category, m_sub_category=EXCLUDED.m_sub_category,
			  collection=EXCLUDED.collection, product_name=EXCLUDED.product_name, finish=EXCLUDED.finish, finish_group=EXCLUDED.finish_group,
			  msrp_cents=EXCLUDED.msrp_cents, map_cents=EXCLUDED.map_cents, dn_cents=EXCLUDED.dn_cents, price_group=EXCLUDED.price_group, currency=EXCLUDED.currency,
			  image_name=EXCLUDED.image_name, image_path=EXCLUDED.image_path, image_type=EXCLUDED.image_type, alt_images=EXCLUDED.alt_images,
			  spec_link=EXCLUDED.spec_link, install_link=EXCLUDED.install_link, web_link=EXCLUDED.web_link, description=EXCLUDED.description,
			  marketing=EXCLUDED.marketing, bullets=EXCLUDED.bullets, attrs=EXCLUDED.attrs, created=EXCLUDED.created, updated=EXCLUDED.updated,
			  file=EXCLUDED.file, loaded_at=now()`,
			pid, r["brandCode"], r["brandName"], sku, NormSKU(sku), r["altSku"], r["model"], r["upc"], r["cx1CategoryID"], r["type"], r["group"], r["category"], r["subCategory"],
			r["mCategory"], r["mSubCategory"], r["collection"], r["productName"], r["finish"], r["finishGroup"], Cents(r["msrp"]), Cents(r["mapPrice"]), Cents(r["dnPrice"]), r["priceGroup"], r["currency"],
			r["imageName"], r["imagePath"], imgType, r.AltImages(), r["avSpecLink"], r["avInstallLink"], r["avWebLink"], r["atDescription"], r["atMarketingCopy"], r.Bullets(), aj, r["createDate"], r["updateDate"], file)
		n++
		if batch.Len() >= 500 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return n, err
	}
	if err := flush(); err != nil {
		return n, err
	}
	// rows from an older file of the same brand that the new file no longer carries are gone
	// from the site: drop them so "current" means current
	if _, err := l.Pool.Exec(ctx, `DELETE FROM feed_products WHERE brand_code=$1 AND file <> $2`, strings.ToUpper(f.Brand), file); err != nil {
		return n, err
	}
	return n, nil
}
