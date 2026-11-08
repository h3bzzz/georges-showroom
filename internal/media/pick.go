// Package media decides which pictures a family shows. The decision is a pure function of
// the evidence (asset_links), the asset measures and the family's category, materialized into
// family_media so pages read one row set and never rank at request time. Every rule here
// came from the 2026 card audits; keep them in Go with tests, not in SQL.
package media

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"georges/internal/assets"
	"georges/internal/taxonomy"
)

// Roles.
const (
	RoleCard      = "card"
	RoleGallery   = "gallery"
	RoleDrawing   = "drawing"
	RoleLifestyle = "lifestyle"
)

// Candidate is one asset linked to a family with everything the rules look at.
type Candidate struct {
	AssetID      int64
	FamilyID     int64
	ItemID       *int64
	How          string
	LinkWeight   float32
	Source       string // vendor | catalog | spec | upload
	Finish       string // the finish a vendor master shows, from its file name ("" = unknown)
	Kind         string // vision verdict
	KindScore    float32
	PKind        string // pixel/OCR verdict from the extractor
	Subject      string
	SubjectScore float32
	Scene        float32
	Width        int
	Height       int
	Quality      float32
	BgWhite      float32
	Cut          float32 // share of the worst frame edge covered by the object
	Built        int16
	PDQ          []byte
	CatalogID    int
	Page         int
	PagePhotos   int // photographs on that catalog page
	PageModels   int // distinct SKUs printed on that page
}

// Pick is one family_media row.
type Pick struct {
	FamilyID int64
	Role     string
	Rank     int16
	AssetID  int64
	ItemID   *int64
	How      string
	Grade    int16
}

// Family is the context the rules need.
type Family struct {
	ID       int64
	BrandID  int
	Category string
	Tier     int
	Title    string
}

// Verdict classifies a candidate's medium after reconciling the vision model with the pixel
// extractor: the model may demote a pixel "photo" when confident and rescue a pixel "drawing"
// it is sure is a photograph.
func Verdict(c Candidate) string {
	switch {
	case c.Source == "vendor":
		return "photo"
	case c.Kind == "" || c.KindScore == 0:
		if c.PKind == "" {
			return "photo"
		}
		return c.PKind
	case c.PKind == "table" || c.PKind == "text" || c.PKind == "blank":
		if c.Kind == "photo" && c.KindScore >= 0.6 && c.Quality >= 0.2 {
			return "photo" // a product photo sitting inside a price grid
		}
		return c.PKind
	case c.Kind == "photo" && c.KindScore >= 0.6:
		return "photo"
	case (c.Kind == "drawing" || c.Kind == "spec") && c.KindScore >= 0.6:
		return "drawing"
	case c.Kind == "callout" && c.KindScore >= 0.5:
		return "drawing"
	case c.Kind == "detail" && c.KindScore >= 0.6:
		return "detail"
	case c.Kind == "lifestyle" && c.KindScore >= 0.5:
		return "lifestyle"
	case (c.Kind == "text" || c.Kind == "other") && c.KindScore >= 0.6:
		return "text"
	}
	if c.PKind != "" {
		return c.PKind
	}
	return c.Kind
}

// cardEligible says whether a candidate may be a family's card photo, and why not.
func cardEligible(c Candidate, f Family) (bool, string) {
	v := Verdict(c)
	if v != "photo" {
		return false, v
	}
	if c.Source == "vendor" {
		return true, ""
	}
	if c.Built == -1 {
		return false, "broken"
	}
	// a swatch, a logo, a person at the tap: never the card
	if (c.Kind == "text" || c.Kind == "other") && c.KindScore >= 0.6 {
		return false, "junk"
	}
	if c.Kind == "lifestyle" && c.KindScore >= 0.5 {
		return false, "lifestyle"
	}
	// the subject must suit the category when the model is sure
	if c.Subject != "" && c.SubjectScore >= 0.7 && !taxonomy.SubjectFits(c.Subject, f.Category) {
		return false, "subject"
	}
	// a crop on a 1700x2200 page is at least ~150 px on its long side
	if c.Source == "catalog" && c.Width > 0 && (max(c.Width, c.Height) < 150 || min(c.Width, c.Height) < 80 || c.Width*c.Height < 12000) {
		return false, "tiny"
	}
	if c.Width > 0 && c.Height > 0 {
		ar := float64(c.Width) / float64(c.Height)
		if ar < 0.33 || ar > 2.4 {
			return false, "aspect"
		}
	}
	// a page-level link is trusted only on a page that shows one photo or at most two models
	if c.How == "page" && !(c.PagePhotos == 1 || c.PageModels <= 2) {
		return false, "page"
	}
	// an object running off the frame (a faucet sliced at a column rule) is never the card
	if c.Cut >= 0.3 {
		return false, "cut"
	}
	return true, ""
}

// cardScore ranks eligible card candidates: provenance first, then the picture itself.
func cardScore(c Candidate, f Family) float64 {
	s := 0.0
	switch c.Source {
	case "vendor":
		s += 3
		// the card follows the catalog convention: the chrome or nickel master when the
		// maker offers one, so a family does not open on its rarest colour
		if c.Finish != "" {
			if taxonomy.FinishTone(c.Finish) == "silver" {
				s += 0.6
			} else {
				s += 0.2
			}
		}
	case "spec":
		s += 1.2
	}
	s += float64(c.LinkWeight) * 1.5
	if c.Kind == "photo" {
		s += float64(c.KindScore) * 0.8
	}
	if c.Kind == "detail" {
		s -= float64(c.KindScore) * 1.5
	}
	if c.Subject != "" && c.SubjectScore >= 0.45 {
		if taxonomy.SubjectFits(c.Subject, f.Category) {
			s += 0.4
		} else {
			s -= 1.0
		}
	}
	s -= float64(c.Scene) * 0.8
	if c.Source == "vendor" {
		// a vendor master on a busy background is a room scene, not the product on white
		if c.BgWhite < 0.3 {
			s -= 1.5
		}
	} else {
		s -= float64(c.Cut) * 2
	}
	s += float64(c.Quality) * 1.5
	if c.Width > 0 && min(c.Width, c.Height) >= 120 {
		s += 0.3
	}
	if c.Width*c.Height >= 40000 {
		s += 0.2
	}
	return s
}

// Grade folds a chosen card into the trust scale browse sorts by (0 trusted … 2 doubtful).
func Grade(c Candidate, f Family) int16 {
	if c.Source == "vendor" {
		return 0
	}
	ar := 1.0
	if c.Width > 0 && c.Height > 0 {
		ar = float64(c.Width) / float64(c.Height)
	}
	doubt := (c.Kind == "detail" && c.KindScore >= 0.3) ||
		((c.Kind == "callout" || c.Kind == "drawing" || c.Kind == "spec" || c.Kind == "text" || c.Kind == "other" || c.Kind == "lifestyle") && c.KindScore >= 0.4) ||
		ar < 0.33 || ar > 2.4 ||
		(c.Width > 0 && min(c.Width, c.Height) < 100) ||
		(c.Subject != "" && c.SubjectScore >= 0.45 && !taxonomy.SubjectFits(c.Subject, f.Category)) ||
		c.Scene >= 0.5 || c.Cut >= 0.15
	if doubt {
		return 2
	}
	strong := c.How == "vendor" || c.How == "sku" || c.How == "exact" || c.How == "family" || c.How == "norm" || c.How == "suffix" || c.How == "spec"
	if strong && c.Kind == "photo" && c.KindScore >= 0.6 &&
		(c.Subject == "" || c.SubjectScore < 0.45 || taxonomy.SubjectFits(c.Subject, f.Category)) &&
		(c.Width == 0 || min(c.Width, c.Height) >= 120) {
		return 0
	}
	return 1
}

// Choose returns the media rows for one family given its candidates.
func Choose(f Family, cands []Candidate) []Pick {
	var photos, drawings, lifestyle []Candidate
	for _, c := range cands {
		switch Verdict(c) {
		case "photo", "detail":
			photos = append(photos, c)
		case "drawing":
			drawings = append(drawings, c)
		case "lifestyle":
			lifestyle = append(lifestyle, c)
		}
	}
	var out []Pick
	// card
	var eligible []Candidate
	for _, c := range photos {
		if ok, _ := cardEligible(c, f); ok {
			eligible = append(eligible, c)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		si, sj := cardScore(eligible[i], f), cardScore(eligible[j], f)
		if si != sj {
			return si > sj
		}
		return eligible[i].Width*eligible[i].Height > eligible[j].Width*eligible[j].Height
	})
	seen := map[int64]bool{}
	var dedupe []Candidate
	if len(eligible) > 0 {
		card := eligible[0]
		out = append(out, Pick{f.ID, RoleCard, 0, card.AssetID, card.ItemID, card.How, Grade(card, f)})
		seen[card.AssetID] = true
		dedupe = append(dedupe, card)
	}
	// gallery: every distinct photograph, the card first, vendor masters (other finishes)
	// next, then catalog photos by score, detail shots last; near-duplicates collapse
	rest := append([]Candidate{}, eligible...)
	for _, c := range photos {
		// a crop vetoed for shape or provenance may still be shown, a sliced one never
		if ok, why := cardEligible(c, f); !ok && (c.Source == "vendor" || c.Cut < 0.3) && (why == "aspect" || why == "page" || why == "detail" || why == "tiny") {
			rest = append(rest, c)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		vi, vj := rest[i].Source == "vendor", rest[j].Source == "vendor"
		if vi != vj {
			return vi
		}
		di, dj := Verdict(rest[i]) == "detail", Verdict(rest[j]) == "detail"
		if di != dj {
			return !di
		}
		ci, cj := rest[i].Cut >= 0.3, rest[j].Cut >= 0.3
		if ci != cj {
			return !ci
		}
		return cardScore(rest[i], f) > cardScore(rest[j], f)
	})
	rank := int16(0)
	for _, c := range rest {
		if seen[c.AssetID] || nearDup(c, dedupe) {
			continue
		}
		seen[c.AssetID] = true
		dedupe = append(dedupe, c)
		out = append(out, Pick{f.ID, RoleGallery, rank, c.AssetID, c.ItemID, c.How, Grade(c, f)})
		rank++
		if rank >= 12 {
			break
		}
	}
	// drawings: largest linked dimension drawing first
	sort.SliceStable(drawings, func(i, j int) bool {
		if drawings[i].LinkWeight != drawings[j].LinkWeight {
			return drawings[i].LinkWeight > drawings[j].LinkWeight
		}
		return drawings[i].Width*drawings[i].Height > drawings[j].Width*drawings[j].Height
	})
	rank = 0
	for _, c := range drawings {
		if seen[c.AssetID] {
			continue
		}
		seen[c.AssetID] = true
		out = append(out, Pick{f.ID, RoleDrawing, rank, c.AssetID, c.ItemID, c.How, 1})
		rank++
		if rank >= 4 {
			break
		}
	}
	rank = 0
	for _, c := range lifestyle {
		if seen[c.AssetID] {
			continue
		}
		seen[c.AssetID] = true
		out = append(out, Pick{f.ID, RoleLifestyle, rank, c.AssetID, c.ItemID, c.How, 1})
		rank++
		if rank >= 4 {
			break
		}
	}
	return out
}

func nearDup(c Candidate, chosen []Candidate) bool {
	if len(c.PDQ) != 32 {
		return false
	}
	for _, o := range chosen {
		if assets.PDQSame(c.PDQ, o.PDQ) {
			return true
		}
	}
	return false
}

// Picker runs the decision over the whole catalog.
type Picker struct {
	Pool *pgxpool.Pool
}

// Run rebuilds family_media. The fallbacks for families with no picture of their own
// (collection opener, same-title twin) run after the direct pass in SQL, because they look
// across families.
func (p *Picker) Run(ctx context.Context) (int, error) {
	start := time.Now()
	fams := map[int64]Family{}
	rows, err := p.Pool.Query(ctx, `SELECT id, brand_id, COALESCE(category,''), tier, COALESCE(title,'') FROM families`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.ID, &f.BrandID, &f.Category, &f.Tier, &f.Title); err != nil {
			return 0, err
		}
		fams[f.ID] = f
	}
	rows.Close()

	brandCodes := map[int]string{}
	br, err := p.Pool.Query(ctx, `SELECT id, COALESCE(code,'') FROM brands`)
	if err != nil {
		return 0, err
	}
	for br.Next() {
		var id int
		var code string
		if err := br.Scan(&id, &code); err != nil {
			br.Close()
			return 0, err
		}
		brandCodes[id] = code
	}
	br.Close()
	finishCodes := map[string]map[string]string{} // brand code → finish code → finish name
	fr, err := p.Pool.Query(ctx, `SELECT brand_code, code, finish FROM finish_codes`)
	if err != nil {
		return 0, err
	}
	for fr.Next() {
		var b, c, f string
		if err := fr.Scan(&b, &c, &f); err != nil {
			fr.Close()
			return 0, err
		}
		if finishCodes[b] == nil {
			finishCodes[b] = map[string]string{}
		}
		finishCodes[b][strings.ToUpper(c)] = f
	}
	fr.Close()

	cr, err := p.Pool.Query(ctx, `
		WITH pages AS (
		  SELECT (origin->>'catalog_id')::int AS catalog_id, (origin->>'page')::int AS page,
		         count(*) FILTER (WHERE kind='photo' OR (kind IS NULL AND origin->>'pkind'='photo')) AS photos
		  FROM assets WHERE source='catalog' GROUP BY 1,2
		), models AS (
		  SELECT catalog_id, page, count(DISTINCT sku_norm) AS n FROM catalog_skus GROUP BY 1,2
		)
		SELECT al.asset_id, al.family_id, al.item_id, al.how, al.weight,
		       a.source, COALESCE(a.origin->>'url',''), COALESCE(a.origin->>'finish_code',''), COALESCE(a.kind,''), COALESCE(a.kind_score,0), COALESCE(a.origin->>'pkind',''),
		       COALESCE(a.subject,''), COALESCE(a.subject_score,0), COALESCE(a.scene,0),
		       COALESCE(a.width,0), COALESCE(a.height,0), COALESCE(a.quality, (a.origin->>'lq')::real, 0.3), COALESCE(a.bg_white,0), COALESCE(a.cut,0), a.built, a.pdq,
		       COALESCE((a.origin->>'catalog_id')::int,0), COALESCE((a.origin->>'page')::int,0), COALESCE(pg.photos,0), COALESCE(m.n,0)
		FROM asset_links al
		JOIN assets a ON a.id = al.asset_id
		LEFT JOIN pages pg ON pg.catalog_id=(a.origin->>'catalog_id')::int AND pg.page=(a.origin->>'page')::int
		LEFT JOIN models m ON m.catalog_id=pg.catalog_id AND m.page=pg.page
		WHERE NOT a.rejected AND a.dup_of IS NULL
		ORDER BY al.family_id`)
	if err != nil {
		return 0, err
	}
	var picks []Pick
	var cur []Candidate
	var curFam int64 = -1
	flush := func() {
		if curFam >= 0 {
			if f, ok := fams[curFam]; ok {
				picks = append(picks, Choose(f, cur)...)
			}
		}
		cur = cur[:0]
	}
	nc := 0
	for cr.Next() {
		var c Candidate
		var url, fcode string
		if err := cr.Scan(&c.AssetID, &c.FamilyID, &c.ItemID, &c.How, &c.LinkWeight, &c.Source, &url, &fcode, &c.Kind, &c.KindScore, &c.PKind,
			&c.Subject, &c.SubjectScore, &c.Scene, &c.Width, &c.Height, &c.Quality, &c.BgWhite, &c.Cut, &c.Built, &c.PDQ,
			&c.CatalogID, &c.Page, &c.PagePhotos, &c.PageModels); err != nil {
			return 0, err
		}
		if c.Source == "vendor" {
			if f, ok := fams[c.FamilyID]; ok {
				codes := finishCodes[brandCodes[f.BrandID]]
				if fcode != "" && codes[strings.ToUpper(fcode)] != "" {
					c.Finish = codes[strings.ToUpper(fcode)]
				} else {
					c.Finish = taxonomy.PicturedFinish(codes, url)
				}
			}
		}
		if c.FamilyID != curFam {
			flush()
			curFam = c.FamilyID
		}
		cur = append(cur, c)
		nc++
	}
	cr.Close()
	flush()
	slog.Info("media pick", "families", len(fams), "candidates", nc, "picks", len(picks))

	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `TRUNCATE family_media`); err != nil {
		return 0, err
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"family_media"}, []string{"family_id", "role", "rank", "asset_id", "item_id", "how", "grade"},
		pgx.CopyFromSlice(len(picks), func(i int) ([]any, error) {
			k := picks[i]
			return []any{k.FamilyID, k.Role, k.Rank, k.AssetID, k.ItemID, k.How, k.Grade}, nil
		}))
	if err != nil {
		return 0, fmt.Errorf("copy: %w", err)
	}
	// fallback 1: a spec-sheet photograph the family has none of its own is handled by the
	// direct pass (spec links are candidates). Fallback 2: the collection opener — a family
	// with no card, placed on a catalog page by a link or a printed SKU, borrows the nearest
	// photograph on the four pages above it in the same book, provided no printed section
	// heading sits between, the opener page shows at most two photos, and the family is not
	// a part.
	series, err := tx.Exec(ctx, `
		WITH placed AS (
		  SELECT f.id AS family_id, x.catalog_id, min(x.page) AS page
		  FROM families f
		  JOIN LATERAL (
		    SELECT (a.origin->>'catalog_id')::int AS catalog_id, (a.origin->>'page')::int AS page
		    FROM asset_links al JOIN assets a ON a.id=al.asset_id
		    WHERE al.family_id=f.id AND a.source='catalog'
		    UNION ALL
		    SELECT k.catalog_id, k.page FROM items i JOIN catalog_skus k ON k.sku_norm=i.sku_norm
		    JOIN catalogs c ON c.id=k.catalog_id AND c.brand_id=i.brand_id
		    WHERE i.family_id=f.id
		  ) x ON true
		  WHERE f.tier <> 2 AND NOT (COALESCE(f.category,'') = ANY($1::text[]))
		    AND NOT EXISTS (SELECT 1 FROM family_media fm WHERE fm.family_id=f.id AND fm.role='card')
		  GROUP BY f.id, x.catalog_id
		), openers AS (
		  SELECT a.id, (a.origin->>'catalog_id')::int AS catalog_id, (a.origin->>'page')::int AS page,
		         a.width*a.height AS area, a.subject, COALESCE(a.subject_score,0) AS subscore
		  FROM assets a
		  WHERE a.source='catalog' AND NOT a.rejected AND a.dup_of IS NULL AND a.built <> -1
		    AND a.kind='photo' AND COALESCE(a.kind_score,0) >= 0.6
		    AND COALESCE(a.width,0) >= 300 AND COALESCE(a.height,0) >= 200
		    AND a.width::real/a.height BETWEEN 0.33 AND 2.4 AND COALESCE(a.scene,0) < 0.5 AND COALESCE(a.cut,0) < 0.2
		    AND (SELECT count(*) FROM assets o WHERE o.source='catalog' AND o.origin->>'catalog_id'=a.origin->>'catalog_id' AND o.origin->>'page'=a.origin->>'page' AND o.kind='photo') <= 2
		), pick AS (
		  SELECT DISTINCT ON (pl.family_id) pl.family_id, o.id AS asset_id
		  FROM placed pl JOIN families f ON f.id=pl.family_id
		  JOIN openers o ON o.catalog_id=pl.catalog_id AND o.page BETWEEN pl.page-4 AND pl.page
		  WHERE (o.subject IS NULL OR o.subscore < 0.45 OR subject_fits(o.subject, f.category))
		    AND NOT EXISTS (SELECT 1 FROM catalog_toc t WHERE t.catalog_id=o.catalog_id AND t.source='printed' AND t.page > o.page AND t.page <= pl.page)
		  ORDER BY pl.family_id, (pl.page - o.page) ASC, o.area DESC
		)
		INSERT INTO family_media (family_id, role, rank, asset_id, how, grade)
		SELECT family_id, 'card', 0, asset_id, 'opener', 1 FROM pick
		ON CONFLICT DO NOTHING`, partCategories())
	if err != nil {
		return 0, fmt.Errorf("series fallback: %w", err)
	}
	// fallback 3: a same-brand family with the same title lends its own card (never one it
	// borrowed itself, never a part's)
	twin, err := tx.Exec(ctx, `
		WITH need AS (
		  SELECT f.id, f.brand_id, f.title FROM families f
		  WHERE f.title IS NOT NULL AND f.title <> '' AND f.tier <> 2
		    AND NOT EXISTS (SELECT 1 FROM family_media fm WHERE fm.family_id=f.id AND fm.role='card')
		), pick AS (
		  SELECT DISTINCT ON (n.id) n.id AS family_id, fm.asset_id, fm.grade
		  FROM need n
		  JOIN families f2 ON f2.brand_id=n.brand_id AND f2.title=n.title AND f2.id<>n.id AND f2.tier<>2
		  JOIN family_media fm ON fm.family_id=f2.id AND fm.role='card' AND fm.how NOT IN ('opener','twin')
		  ORDER BY n.id, fm.grade, fm.asset_id
		)
		INSERT INTO family_media (family_id, role, rank, asset_id, how, grade)
		SELECT family_id, 'card', 0, asset_id, 'twin', GREATEST(grade, 1) FROM pick
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, fmt.Errorf("twin fallback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	slog.Info("media pick done", "direct", len(picks), "openers", series.RowsAffected(), "twins", twin.RowsAffected(), "took", time.Since(start).Round(time.Millisecond))
	return len(picks) + int(series.RowsAffected()) + int(twin.RowsAffected()), nil
}

func partCategories() []string {
	out := make([]string, 0, len(taxonomy.PartCategories))
	for c := range taxonomy.PartCategories {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
