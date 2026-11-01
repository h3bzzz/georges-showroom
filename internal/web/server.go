// Package web is the new George's storefront: chi router, templ views, htmx partials.
package web

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/image/draw"
	"html"
	"image"
	"image/jpeg"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"georges_/internal/imgsrv"
	"georges_/internal/ocr"
	"georges_/internal/specs"
	"georges_/internal/store"
	"georges_/internal/web/views"
)

//go:embed static/*
var static embed.FS

type Server struct {
	st      *store.Store
	dataDir string
	img     *imgsrv.Server
	r       chi.Router

	marks sync.Map // logo path -> markHash, so the home page does not re-read every logo per request
}

type markHash struct {
	mod time.Time
	sum string
}

func New(st *store.Store, dataDir string) *Server {
	s := &Server{st: st, dataDir: dataDir, img: imgsrv.New(dataDir)}
	r := chi.NewRouter()
	// no RealIP: it rewrites RemoteAddr from X-Forwarded-For / X-Real-IP, which would let any
	// remote client pass loopbackOnly by sending the header
	r.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer, middleware.Compress(5), middleware.GetHead)
	r.Use(middleware.Timeout(30 * time.Second))

	sub, _ := fs.Sub(static, "static")
	if css, err := fs.ReadFile(static, "static/app.css"); err == nil {
		sum := sha256.Sum256(css)
		views.AssetVersion = hex.EncodeToString(sum[:4])
	}
	r.Handle("/static/*", cacheFor(24*time.Hour, http.StripPrefix("/static/", http.FileServer(http.FS(sub)))))
	// images only, allow-listed roots, no directory listings: DATA_DIR also holds Postgres and price files
	r.Get("/files/*", s.img.Original)
	r.Get("/img/*", s.img.Resized)

	r.Get("/", s.home)
	r.Get("/brands", s.brands)
	r.Get("/brand/{slug}", s.brand)
	r.Get("/shop", s.shop)
	r.Get("/shop/{room}", s.shop)
	r.Get("/products", s.products)
	r.Get("/product/{id}", s.product)
	r.Get("/search", s.search)
	r.Get("/catalogs", s.catalogs)
	r.Get("/catalog/{id}", s.catalog)
	r.Get("/catalog/{id}/page/{n}", s.catalogPage)
	r.Get("/catalog/{id}/thumb", s.catalogThumb)
	r.Get("/catalog/{id}/cover", s.catalogCover)
	r.Get("/catalog/{id}/page/{n}/text", s.catalogPageText)
	r.Get("/sku/{sku}", s.sku)
	r.Get("/doc/{id}", s.doc)
	r.Group(func(r chi.Router) {
		r.Use(loopbackOnly) // curation + uploads: this machine only until there is real auth
		r.Get("/admin/covers", s.adminCovers)
		r.Post("/admin/covers", s.adminSetCover)
		r.Options("/admin/logos/upload", s.adminLogoUpload)
		r.Post("/admin/logos/upload", s.adminLogoUpload)
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	s.r = r
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.r.ServeHTTP(w, r) }

// loopbackOnly rejects requests whose TCP peer is not this machine (RemoteAddr is never
// rewritten from headers, see New).
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "admin routes are local-only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cacheFor(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(d.Seconds())))
		next.ServeHTTP(w, r)
	})
}

// roomCovers picks one real catalog cover per room for the landing tiles.
var roomCovers = []struct {
	Room, Href string
	Books      [][2]string
}{
	{"Bathroom", "/shop/bathroom", [][2]string{{"GES", "GES_Bathroom_Collection"}, {"DRV", "PL26_Duravit_Pricebook"}, {"KOL", "KOL_Fixture_2025_Pricebook"}}},
	{"Kitchen", "/shop/kitchen", [][2]string{{"GES", "Art_for_Chefs"}, {"BLA", "BLA-2025PriceBook"}, {"CAL", "CAL-2026Pricebook"}}},
	{"Hardware", "/shop/hardware", [][2]string{{"ETK", "ETK_2026_Door_Hardware_Price_Book"}, {"RMH", "RMH_Full-Line-Brochure"}, {"BAL", "BAL_EstatePriceBook-2025"}}},
	{"Appliances", "/shop/appliances", [][2]string{{"WFF", "2022JanDesignGuide"}, {"ZRO", "2021NovDesign Guide"}, {"BSH", "BoschRefrigeration2019Brochure"}, {"THE", "2020LookbookVolume7"}}},
}

// roomTile picks the first candidate book with a lifestyle spread; else the first book's cover.
func (s *Server) roomTile(ctx context.Context, room, href string, books [][2]string) views.RoomTile {
	t := views.RoomTile{Room: room, Href: href, Page: 1}
	slug := strings.ToLower(room)
	if cur, _ := s.st.GetCover(ctx, "room", slug); cur.Path != "" {
		t.Image = "/files/" + cur.Path
		if cat, _ := s.st.CatalogByID(ctx, cur.CatalogID); cat != nil {
			t.CatalogID, t.Caption, t.Page = cat.ID, cat.Title, cur.Page
		}
		return t
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if _, err := os.Stat(filepath.Join(s.dataDir, "covers", slug+ext)); err == nil {
			t.Image, t.Caption = "/files/covers/"+slug+ext, "George's"
			return t
		}
	}
	for _, b := range books {
		cat, _ := s.st.CatalogByBook(ctx, b[0], b[1])
		if cat == nil {
			continue
		}
		if t.CatalogID == 0 {
			t.CatalogID, t.Caption = cat.ID, cat.Title
		}
		if cv, _ := s.st.LifestyleCrop(ctx, cat.ID); cv.Path != "" {
			t.CatalogID, t.Caption, t.Page, t.Image = cat.ID, cat.Title, cv.Page, "/files/"+cv.Path
			break
		}
		if lp := s.st.LifestylePage(ctx, cat.ID); lp > 0 {
			t.CatalogID, t.Caption, t.Page = cat.ID, cat.Title, lp
			break
		}
	}
	return t
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, f, p, c, pg, _ := s.st.Counts(ctx)
	brands, _ := s.st.ListBrands(ctx)
	books, _ := s.st.BrandBooks(ctx)
	tiles := s.st.HomeTileViews(ctx)
	var hero []views.HeroBook
	for _, want := range [][2]string{{"CAL", "CAL-2026Pricebook"}, {"GRF", "GRF_Bathroom_Price_Book"}, {"HYS", "HYS-2025Pricelist"}, {"ASH", "ASH_2026_ManzoniPricedCatalog"}} {
		if cat, _ := s.st.CatalogByBook(ctx, want[0], want[1]); cat != nil {
			hb := views.HeroBook{ID: cat.ID, Title: cat.Title, Image: "/catalog/" + strconv.Itoa(cat.ID) + "/cover"}
			hero = append(hero, hb)
		}
	}
	// the home wall shows every maker that has a real mark, like the old site's Featured Brands
	allCards, _ := s.st.BrandCards(ctx, false)
	var featured []store.BrandCard
	for _, c := range allCards {
		if c.Logo != "" {
			featured = append(featured, c)
		}
	}
	featured = s.dedupeMarks(featured)
	s.logoVersion(featured)
	views.Home(views.Stats{Brands: b, Families: f, Products: p, Catalogs: c, Pages: pg}, hero, tiles, books, brands, featured).Render(ctx, w)
}

// logoVersion appends the logo file's mtime so a replaced mark bypasses the browser cache.
func (s *Server) logoVersion(cards []store.BrandCard) {
	for i := range cards {
		p := strings.TrimPrefix(cards[i].Logo, "/files/")
		if p == cards[i].Logo || p == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(s.dataDir, filepath.FromSlash(p))); err == nil {
			cards[i].Logo += "?v=" + strconv.FormatInt(st.ModTime().Unix(), 10)
		}
	}
}

// dedupeMarks keeps one tile per distinct logo file (sister brands often share a mark),
// preferring the brand that actually has products or books.
func (s *Server) dedupeMarks(cards []store.BrandCard) []store.BrandCard {
	best := map[string]int{}
	for i, c := range cards {
		p := strings.TrimPrefix(strings.SplitN(c.Logo, "?", 2)[0], "/files/")
		key := s.markKey(p)
		if j, ok := best[key]; !ok || c.Families+c.Catalogs > cards[j].Families+cards[j].Catalogs {
			best[key] = i
		}
	}
	keep := map[int]bool{}
	for _, i := range best {
		keep[i] = true
	}
	var out []store.BrandCard
	for i, c := range cards {
		if keep[i] {
			out = append(out, c)
		}
	}
	return out
}

// markKey identifies a logo by content so sister brands sharing one file collapse to one tile;
// the hash is cached per path and recomputed only when the file changes.
func (s *Server) markKey(rel string) string {
	full := filepath.Join(s.dataDir, filepath.FromSlash(rel))
	st, err := os.Stat(full)
	if err != nil {
		return rel
	}
	if v, ok := s.marks.Load(rel); ok && v.(markHash).mod.Equal(st.ModTime()) {
		return v.(markHash).sum
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return rel
	}
	sum := sha1.Sum(b)
	h := markHash{mod: st.ModTime(), sum: hex.EncodeToString(sum[:])}
	s.marks.Store(rel, h)
	return h.sum
}

func (s *Server) brands(w http.ResponseWriter, r *http.Request) {
	all, err := s.st.BrandCards(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.logoVersion(all)
	room := r.URL.Query().Get("room")
	counts := map[string]int{}
	var cards []store.BrandCard
	for _, c := range all {
		in := room == ""
		for _, rm := range c.Rooms {
			counts[rm]++
			if rm == room {
				in = true
			}
		}
		if in {
			cards = append(cards, c)
		}
	}
	views.Brands(cards, room, counts, len(all)).Render(r.Context(), w)
}

func (s *Server) brand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.st.BrandBySlug(ctx, chi.URLParam(r, "slug"))
	if err != nil || b == nil {
		http.NotFound(w, r)
		return
	}
	page := atoi(r.URL.Query().Get("page"), 1)
	cat := r.URL.Query().Get("category")
	fams, total, err := s.st.ListFamilies(ctx, store.FamilyFilter{BrandSlug: b.Slug, Category: cat, Page: page})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cats, _ := s.st.TopCategories(ctx, b.Slug, 40)
	views.Brand(*b, fams, cats, cat, views.Pager{Page: page, Total: total, PerPage: 24, Base: "/brand/" + b.Slug + "?category=" + cat}).Render(ctx, w)
}

// shop is the faceted browse page behind the room links.
func (s *Server) shop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.BrowseFilter{
		Room: chi.URLParam(r, "room"), Category: q.Get("category"), Query: q.Get("q"), Sort: q.Get("sort"),
		Exact: q.Get("exact") != "",
		Page: min(max(atoi(q.Get("page"), 1), 1), 500), PerPage: 24,
	}
	for _, b := range q["brand"] {
		if b != "" {
			f.Brands = append(f.Brands, b)
		}
	}
	for _, x := range q["finish"] {
		if x != "" {
			f.Finishes = append(f.Finishes, x)
		}
	}
	for _, sp := range q["spec"] {
		if k, v, ok := strings.Cut(sp, ":"); ok && k != "" && v != "" {
			if f.Specs == nil {
				f.Specs = map[string][]string{}
			}
			f.Specs[k] = append(f.Specs[k], v)
		}
	}
	if v := atoi(q.Get("min"), 0); v > 0 {
		f.MinCents = int64(v) * 100
	}
	if v := atoi(q.Get("max"), 0); v > 0 {
		f.MaxCents = int64(v) * 100
	}
	valid := f.Room == ""
	for _, rm := range store.Rooms {
		if rm.Slug == f.Room {
			valid = true
		}
	}
	if !valid {
		http.NotFound(w, r)
		return
	}
	res, err := s.st.Browse(ctx, f)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	rooms, _ := s.st.RoomCounts(ctx)
	var banner views.RoomTile
	var tiles []store.CategoryTile
	for _, rc := range roomCovers {
		if strings.EqualFold(rc.Room, f.Room) {
			banner = s.roomTile(ctx, rc.Room, rc.Href, rc.Books)
			if f.Category == "" && f.Query == "" && len(f.Brands) == 0 {
				tiles, _ = s.st.CategoryTiles(ctx, f.Room, 8)
			}
		}
	}
	views.Shop(f, res, views.ShopURL{F: f}, rooms, banner, tiles).Render(ctx, w)
}

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	// legacy: send old links to the shop page, mapping ?category=Bathroom etc. to rooms
	q := r.URL.Query()
	cat := q.Get("category")
	for _, rm := range store.Rooms {
		if strings.EqualFold(cat, rm.Name) {
			q.Del("category")
			http.Redirect(w, r, "/shop/"+rm.Slug+"?"+q.Encode(), http.StatusFound)
			return
		}
	}
	http.Redirect(w, r, "/shop?"+q.Encode(), http.StatusFound)
}

func (s *Server) productsLegacy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	page := atoi(q.Get("page"), 1)
	f := store.FamilyFilter{BrandSlug: q.Get("brand"), Category: q.Get("category"), Query: q.Get("q"), Page: page}
	fams, total, err := s.st.ListFamilies(ctx, f)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	base := "/products?q=" + q.Get("q") + "&brand=" + q.Get("brand") + "&category=" + q.Get("category")
	pager := views.Pager{Page: page, Total: total, PerPage: 24, Base: base}
	if r.Header.Get("HX-Request") != "" {
		views.ProductGrid(fams, pager).Render(ctx, w)
		return
	}
	cats, _ := s.st.TopCategories(ctx, "", 30)
	views.Products(fams, cats, f, pager).Render(ctx, w)
}

func (s *Server) product(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	fam, err := s.st.FamilyByID(ctx, id)
	if err != nil || fam == nil {
		http.NotFound(w, r)
		return
	}
	skus, _ := s.st.ProductsForFamily(ctx, id)
	docs, _ := s.st.DocumentsForFamily(ctx, id)
	imgs, _ := s.st.ImagesForFamily(ctx, id)
	pages, _ := s.st.PagesForFamily(ctx, id, 8)
	drawings, _ := s.st.DrawingsForFamily(ctx, id)
	sibs, _ := s.st.Siblings(ctx, *fam, 8)
	views.Product(*fam, skus, docs, imgs, pages, drawings, sibs).Render(ctx, w)
}

// search is the htmx endpoint behind the header search box.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		w.Write([]byte(""))
		return
	}
	fams, total, _ := s.st.ListFamilies(ctx, store.FamilyFilter{Query: q, PerPage: 8})
	hits, _ := s.st.SearchCatalogText(ctx, q, 5)
	// a model number no product carries yet (its book not read, or a printed cross-reference):
	// the pages that print it, and the SKU page that gathers every crop and mention
	skuURL := ""
	var skuHits []store.CatalogHit
	if k := ocr.Normalize(strings.Join(strings.Fields(q), "")); ocr.LooksLikeSKU(k) {
		skuURL = "/sku/" + url.PathEscape(k)
		if len(fams) == 0 {
			skuHits, _ = s.st.SKUPages(ctx, k, 6)
			// the same page found again by its text is noise next to the SKU hit
			seen := map[[2]int]bool{}
			for _, h := range skuHits {
				seen[[2]int{h.CatalogID, h.Page}] = true
			}
			kept := hits[:0]
			for _, h := range hits {
				if !seen[[2]int{h.CatalogID, h.Page}] {
					kept = append(kept, h)
				}
			}
			hits = kept
		}
	}
	views.SearchResults(q, fams, total, hits, skuHits, skuURL).Render(ctx, w)
}

func (s *Server) catalogs(w http.ResponseWriter, r *http.Request) {
	groups, err := s.st.CatalogGroups(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if code := r.URL.Query().Get("brand"); code != "" {
		kept := groups[:0]
		for _, g := range groups {
			if strings.EqualFold(g.Code, code) {
				kept = append(kept, g)
			}
		}
		groups = kept
	}
	views.Catalogs(groups).Render(r.Context(), w)
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := atoi(chi.URLParam(r, "id"), 0)
	c, err := s.st.CatalogByID(ctx, id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	n := atoi(r.URL.Query().Get("p"), 1)
	if n < 1 {
		n = 1
	}
	if c.Pages > 0 && n > c.Pages {
		n = c.Pages
	}
	tocs, _ := s.st.TOC(ctx, id)
	skus, _ := s.st.PageSKUMap(ctx, id)
	ocrd := 0
	if len(tocs) == 0 {
		ocrd = s.st.CatalogProgress(ctx, id)
	}
	views.Catalog(*c, n, tocs, skus, ocrd).Render(ctx, w)
}

// adminCovers is the curation page: for a room, its banner and category tiles with candidates.
func (s *Server) adminCovers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	room := r.URL.Query().Get("room")
	if room == "" {
		room = "bathroom"
	}
	if room == "home" {
		s.adminHomeTiles(w, r)
		return
	}
	var rc views.RoomTile
	for _, c := range roomCovers {
		if strings.EqualFold(c.Room, room) {
			rc = s.roomTile(ctx, c.Room, c.Href, c.Books)
		}
	}
	roomCands, _ := s.st.CoverCandidates(ctx, room, "", 18)
	tiles, _ := s.st.CategoryTiles(ctx, room, 12)
	var sets []views.CoverSet
	for _, t := range tiles {
		cands, _ := s.st.CoverCandidates(ctx, room, t.Name, 10)
		sets = append(sets, views.CoverSet{Tile: t, Candidates: cands})
	}
	views.AdminCovers(room, rc, roomCands, sets).Render(ctx, w)
}

// adminHomeTiles curates the landing tile photographs; tiles are resolved fresh (no memo) so a
// pick shows at once, and the candidates are the same pool the automatic choice draws from.
func (s *Server) adminHomeTiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	used := map[string]bool{}
	sets := make([]views.HomeCoverSet, 0, len(store.HomeTiles))
	for _, t := range store.HomeTiles {
		sets = append(sets, views.HomeCoverSet{Tile: s.st.ResolveHomeTile(ctx, t, used), Candidates: s.st.HomeTileCandidates(ctx, t, 12)})
	}
	views.AdminHomeTiles(sets).Render(ctx, w)
}

// adminSetCover stores a pick (or clears it) and re-renders the affected block.
func (s *Server) adminSetCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = r.ParseForm()
	kind, key, path := r.FormValue("kind"), r.FormValue("key"), r.FormValue("path")
	cat, page := atoi(r.FormValue("catalog_id"), 0), atoi(r.FormValue("page"), 0)
	if r.FormValue("clear") != "" {
		_ = s.st.ClearCover(ctx, kind, key)
	} else if path != "" {
		_ = s.st.SetCover(ctx, kind, key, path, cat, page)
	}
	room := r.FormValue("room")
	http.Redirect(w, r, "/admin/covers?room="+room+"#"+views.AnchorFor(kind, key), http.StatusSeeOther)
}

// sku shows everything we know about one SKU string: catalog crops and pages mentioning it.
func (s *Server) sku(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sku := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "sku")))
	crops, _ := s.st.CropsForSKUView(ctx, sku)
	pages, _ := s.st.PagesMentioning(ctx, sku, 30)
	views.SKU(sku, crops, pages).Render(ctx, w)
}

// catalogPage serves a page image from DATA_DIR/catalogs/<CODE>/<name>/NNNN.jpg.
func (s *Server) catalogPage(w http.ResponseWriter, r *http.Request) {
	id := atoi(chi.URLParam(r, "id"), 0)
	n := atoi(chi.URLParam(r, "n"), 0)
	c, err := s.st.CatalogByID(r.Context(), id)
	if err != nil || c == nil || n < 1 {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.dataDir, "catalogs", c.BrandCode, safeName(c.Name), fmt.Sprintf("%04d.jpg", n))
	if _, err := os.Stat(path); err != nil {
		// not downloaded yet: redirect to the vendor flipbook image
		http.Redirect(w, r, fmt.Sprintf("https://catalogs.myplumbingshowroom.com/%s/%s/files/large/%d.jpg", c.BrandCode, strings.ReplaceAll(c.Name, " ", "%20"), n), http.StatusFound)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

// catalogPageText renders the OCR words of one page as an invisible, selectable text layer
// positioned over the page image (percent coordinates, so it scales with the image).
func (s *Server) catalogPageText(w http.ResponseWriter, r *http.Request) {
	id := atoi(chi.URLParam(r, "id"), 0)
	n := atoi(chi.URLParam(r, "n"), 0)
	c, err := s.st.CatalogByID(r.Context(), id)
	if err != nil || c == nil || n < 1 {
		http.NotFound(w, r)
		return
	}
	base := filepath.Join(s.dataDir, "catalogs", c.BrandCode, safeName(c.Name), fmt.Sprintf("%04d", n))
	wb, err := os.ReadFile(base + ".words.json")
	if err != nil {
		w.Write([]byte("")) // not OCR'd yet: nothing to overlay
		return
	}
	var words []ocr.Word
	if json.Unmarshal(wb, &words) != nil {
		w.Write([]byte(""))
		return
	}
	pw, ph := 1692, 2206
	if f, err := os.Open(base + ".jpg"); err == nil {
		if cfg, _, err := image.DecodeConfig(f); err == nil {
			pw, ph = cfg.Width, cfg.Height
		}
		f.Close()
	}
	var b strings.Builder
	b.WriteString(`<div class="textlayer" aria-hidden="false">`)
	for _, wd := range words {
		if wd.Conf < 30 || wd.W <= 0 || wd.H <= 0 {
			continue
		}
		hp := 100 * float64(wd.H) / float64(ph)
		// font size as a share of the container *width* (cqw): inline-size containment keeps the
		// page's height (a size container collapses to 0 and stacks every page on the first)
		fs := hp * 0.85 * float64(ph) / float64(pw)
		fmt.Fprintf(&b, `<span style="left:%.2f%%;top:%.2f%%;width:%.2f%%;height:%.2f%%;font-size:%.2fcqw">%s</span>`,
			100*float64(wd.X)/float64(pw), 100*float64(wd.Y)/float64(ph), 100*float64(wd.W)/float64(pw), hp, fs, html.EscapeString(wd.Text))
	}
	b.WriteString(`</div>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(b.String()))
}

func (s *Server) catalogThumb(w http.ResponseWriter, r *http.Request) {
	id := atoi(chi.URLParam(r, "id"), 0)
	c, err := s.st.CatalogByID(r.Context(), id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.dataDir, "catalogs", c.BrandCode, safeName(c.Name), "thumb.jpg")
	if _, err := os.Stat(path); err != nil {
		http.Redirect(w, r, fmt.Sprintf("https://catalogs.myplumbingshowroom.com/%s/%s/files/thumb/1.jpg", c.BrandCode, strings.ReplaceAll(c.Name, " ", "%20")), http.StatusFound)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

// Run serves until ctx is cancelled.
func Run(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// adminLogoUpload receives a brand mark pushed from a browser session (JSON {slug, mime, b64})
// and stores it as the brand's logo. CORS is open because the pushing page is another origin.
func (s *Server) adminLogoUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "content-type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var in struct {
		Slug, Code, Mime, B64 string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&in); err != nil || in.B64 == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if in.Slug == "" && in.Code != "" {
		in.Slug, _ = s.st.BrandSlugByCode(r.Context(), in.Code)
	}
	if in.Slug == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	data, err := base64.StdEncoding.DecodeString(in.B64)
	if err != nil || len(data) < 200 {
		http.Error(w, "bad image", http.StatusBadRequest)
		return
	}
	// no SVG: it is a script container when served as a file
	ext := map[string]string{"image/gif": ".gif", "image/png": ".png", "image/webp": ".webp", "image/jpeg": ".jpg"}[in.Mime]
	if ext == "" {
		http.Error(w, "unsupported type", http.StatusBadRequest)
		return
	}
	slug := store.Slugify(in.Slug)
	dir := filepath.Join(s.dataDir, "logos")
	_ = os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, slug+ext), data, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, old := range []string{".gif", ".png", ".webp", ".jpg", ".svg"} {
		if old != ext {
			_ = os.Remove(filepath.Join(dir, slug+old))
		}
	}
	if err := s.st.SetBrandLogoBySlug(r.Context(), slug, "/files/logos/"+slug+ext); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"ok":true,"path":"/files/logos/%s%s","bytes":%d}`, slug, ext, len(data))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// catalogCover serves a crisp 720px-wide rendition of the book's first page (derived once from
// the full-resolution download and cached as cover.jpg); falls back to the flipbook thumbnail.
func (s *Server) catalogCover(w http.ResponseWriter, r *http.Request) {
	id := atoi(chi.URLParam(r, "id"), 0)
	c, err := s.st.CatalogByID(r.Context(), id)
	if err != nil || c == nil {
		http.NotFound(w, r)
		return
	}
	dir := filepath.Join(s.dataDir, "catalogs", c.BrandCode, safeName(c.Name))
	cover := filepath.Join(dir, "cover.jpg")
	if !fileExists(cover) {
		src := filepath.Join(dir, "0001.jpg")
		if !fileExists(src) {
			s.catalogThumb(w, r)
			return
		}
		if err := resizeJPEG(src, cover, 720); err != nil {
			s.catalogThumb(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, cover)
}

func resizeJPEG(src, dst string, width int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	b := img.Bounds()
	if b.Dx() <= width {
		width = b.Dx()
	}
	h := b.Dy() * width / b.Dx()
	out := image.NewRGBA(image.Rect(0, 0, width, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Over, nil)
	g, err := os.CreateTemp(filepath.Dir(dst), ".cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(g.Name())
	if err := jpeg.Encode(g, out, &jpeg.Options{Quality: 90}); err != nil {
		g.Close()
		return err
	}
	if err := g.Close(); err != nil {
		return err
	}
	return os.Rename(g.Name(), dst)
}

// doc serves a spec/install sheet from the local copy when `specs sync` has fetched it,
// otherwise redirects to the CDN origin (the per-site alias printed on the old pages answers 403).
func (s *Server) doc(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	url, path, err := s.st.DocumentByID(r.Context(), id)
	if err != nil || url == "" {
		http.NotFound(w, r)
		return
	}
	if path != "" {
		full := filepath.Join(s.dataDir, filepath.FromSlash(path))
		if _, err := os.Stat(full); err == nil {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Cache-Control", "public, max-age=604800")
			http.ServeFile(w, r, full)
			return
		}
	}
	http.Redirect(w, r, specs.NormalizeURL(url), http.StatusFound)
}
