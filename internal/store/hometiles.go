package store

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// HomeTile is one merchandised entry point on the landing page. A room tile ("Kitchen") opens
// the room; every other tile opens the shop filtered to a taxonomy group (CategoryGroups), so
// the tile's count is exactly what its click returns.
type HomeTile struct {
	Name     string   // label; also the covers(kind='home') key
	Room     string   // room the click is scoped to ("" = every room)
	Category string   // CategoryGroups key or leaf category ("" for a room tile)
	Photo    string   // word a catalog page about the subject would use (automatic cover search)
	Fallback []string // curated covers to reuse before searching: "room:<slug>" or "category:<room>|<name>"
}

// HomeTiles is the landing tile set, in display order.
var HomeTiles = []HomeTile{
	{Name: "Kitchen", Room: "kitchen", Photo: "kitchen", Fallback: []string{"room:kitchen"}},
	{Name: "Bathroom", Room: "bathroom", Photo: "bath", Fallback: []string{"room:bathroom"}},
	{Name: "Faucets", Category: "Faucets", Photo: "faucet", Fallback: []string{"category:bathroom|Faucets", "category:kitchen|Kitchen Faucets"}},
	{Name: "Showers", Room: "bathroom", Category: "Showers", Photo: "shower", Fallback: []string{"category:bathroom|Showers"}},
	{Name: "Sinks", Category: "Sinks", Photo: "sink", Fallback: []string{"category:bathroom|Sinks", "category:kitchen|Kitchen Sinks"}},
	{Name: "Bathtubs", Room: "bathroom", Category: "Bathtubs", Photo: "tub", Fallback: []string{"category:bathroom|Bathtubs"}},
	{Name: "Toilets", Room: "bathroom", Category: "Toilets", Photo: "toilet", Fallback: []string{"category:bathroom|Toilets"}},
	{Name: "Vanities", Room: "bathroom", Category: "Vanities", Photo: "vanit", Fallback: []string{"category:bathroom|Vanities"}},
	{Name: "Medicine Cabinets", Room: "bathroom", Category: "Medicine Cabinets", Photo: "medicine"},
	{Name: "Mirrors", Room: "bathroom", Category: "Mirrors", Photo: "mirror", Fallback: []string{"category:bathroom|Mirrors"}},
	{Name: "Cabinet Hardware", Room: "hardware", Category: "Cabinet Hardware", Photo: "cabinet", Fallback: []string{"category:hardware|Cabinet Pulls", "category:hardware|Cabinet Knobs"}},
	{Name: "Door Hardware", Room: "hardware", Category: "Door Hardware", Photo: "door", Fallback: []string{"category:hardware|Door Levers", "category:hardware|Entry Sets"}},
}

// Href is the shop URL the tile opens.
func (t HomeTile) Href() string {
	base := "/shop"
	if t.Room != "" {
		base += "/" + t.Room
	}
	if t.Category == "" {
		return base
	}
	return base + "?" + url.Values{"category": {t.Category}}.Encode()
}

// IsRoom reports whether the tile opens a whole room rather than a category.
func (t HomeTile) IsRoom() bool { return t.Category == "" }

// Filter is the browse filter the tile's click applies.
func (t HomeTile) Filter() BrowseFilter { return BrowseFilter{Room: t.Room, Category: t.Category} }

// HomeTileByName finds a tile by its label (nil when unknown).
func HomeTileByName(name string) *HomeTile {
	for i := range HomeTiles {
		if HomeTiles[i].Name == name {
			return &HomeTiles[i]
		}
	}
	return nil
}

// HomeTileView is a tile resolved for rendering.
type HomeTileView struct {
	HomeTile
	Href    string
	Count   int    // families the click returns
	Cover   Cover  // chosen photograph (Path "" when none exists yet)
	Caption string // title of the catalog the photograph comes from
}

// homeTileMinSizes are tried in order: a proper lifestyle spread first, then any usable photo,
// so a tile whose books are still being processed is not left blank.
var homeTileMinSizes = [][2]int{{600, 350}, {360, 240}}

// ResolveHomeTile picks the tile's count and cover: a curated home pick, else a curated cover
// the tile shares with a room page, else the best catalog photograph about the subject that
// no other tile on the page already uses (used may be nil).
func (s *Store) ResolveHomeTile(ctx context.Context, t HomeTile, used map[string]bool) HomeTileView {
	v := HomeTileView{HomeTile: t, Href: t.Href()}
	v.Count, _ = s.CountFamilies(ctx, t.Filter())
	v.Cover = s.homeTileCover(ctx, t, used)
	if v.Cover.CatalogID > 0 {
		if cat, _ := s.CatalogByID(ctx, v.Cover.CatalogID); cat != nil {
			v.Caption = cat.Title
		}
	}
	if used != nil && v.Cover.Path != "" {
		used[v.Cover.Path] = true
	}
	return v
}

func (s *Store) homeTileCover(ctx context.Context, t HomeTile, used map[string]bool) Cover {
	if c, _ := s.GetCover(ctx, "home", t.Name); c.Path != "" {
		return c
	}
	for _, fb := range t.Fallback {
		kind, key, ok := cutKind(fb)
		if !ok {
			continue
		}
		if c, _ := s.GetCover(ctx, kind, key); c.Path != "" && !used[c.Path] {
			return c
		}
	}
	for _, c := range s.HomeTileCandidates(ctx, t, 8) {
		if !used[c.Path] {
			return c
		}
	}
	return Cover{}
}

// HomeTileCandidates lists photographs a person could choose for the tile, best first.
func (s *Store) HomeTileCandidates(ctx context.Context, t HomeTile, limit int) []Cover {
	var out []Cover
	seen := map[string]bool{}
	for _, min := range homeTileMinSizes {
		cands, _ := s.coverCandidates(ctx, t.Room, t.Photo, limit, min[0], min[1])
		for _, c := range cands {
			if !seen[c.Path] {
				seen[c.Path] = true
				out = append(out, c)
			}
		}
		if len(out) >= limit {
			break
		}
	}
	if len(out) < limit {
		for _, c := range s.dividerPages(ctx, t.Room, t.Photo, limit-len(out)) {
			if !seen[c.Path] {
				seen[c.Path] = true
				out = append(out, c)
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// dividerPages finds section-opener pages (a heading and a photograph, almost no other text)
// whose heading names the subject: "Mirrors & Medicine Cabinets" above a room shot. The page
// render itself is the cover, served from the catalogs root.
func (s *Store) dividerPages(ctx context.Context, room, kw string, limit int) []Cover {
	if kw == "" || limit <= 0 {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT p.path, p.catalog_id, p.page, 0, 0
		FROM catalog_pages p JOIN catalogs c ON c.id=p.catalog_id JOIN brands b ON b.code=c.brand_code
		WHERE p.path <> '' AND ($1 = '' OR brand_room(b.id) = $1)
		  AND length(COALESCE(p.text, p.ocr_text, '')) BETWEEN 1 AND 160
		  AND COALESCE(p.text, p.ocr_text, '') ILIKE '%'||$2||'%'
		ORDER BY length(COALESCE(p.text, p.ocr_text, '')), p.catalog_id, p.page LIMIT $3`, room, kw, limit)
	if err != nil {
		return nil
	}
	cands, _ := pgx.CollectRows(rows, pgx.RowToStructByPos[Cover])
	for i := range cands {
		cands[i].Path = dataRelative(cands[i].Path)
	}
	return cands
}

// dataRelative turns a page render path as stored ("data/raw/catalogs/…") into the DATA_DIR-
// relative form covers use ("catalogs/…"), which is what the image server accepts.
func dataRelative(p string) string {
	if i := strings.Index(p, "catalogs/"); i >= 0 {
		return p[i:]
	}
	return p
}

func cutKind(s string) (kind, key string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], i > 0 && i+1 < len(s)
		}
	}
	return "", "", false
}

// The landing page is the most visited page and its tiles cost a dozen counts and cover
// lookups, so the resolved set is memoised; curation (SetCover/ClearCover) drops the memo.
var (
	homeTilesMu   sync.Mutex
	homeTilesMemo []HomeTileView
	homeTilesAt   time.Time
)

const homeTilesTTL = 10 * time.Minute

// HomeTileViews returns the resolved landing tiles, cached for homeTilesTTL.
func (s *Store) HomeTileViews(ctx context.Context) []HomeTileView {
	homeTilesMu.Lock()
	defer homeTilesMu.Unlock()
	if homeTilesMemo != nil && time.Since(homeTilesAt) < homeTilesTTL {
		return homeTilesMemo
	}
	used := map[string]bool{}
	views := make([]HomeTileView, 0, len(HomeTiles))
	for _, t := range HomeTiles {
		views = append(views, s.ResolveHomeTile(ctx, t, used))
	}
	if ctx.Err() != nil {
		return views // a cancelled request must not poison the cache with partial results
	}
	homeTilesMemo, homeTilesAt = views, time.Now()
	return views
}

// InvalidateHomeTiles drops the memoised landing tiles so the next request re-resolves them.
func InvalidateHomeTiles() {
	homeTilesMu.Lock()
	homeTilesMemo = nil
	homeTilesMu.Unlock()
}
