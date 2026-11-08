// Package web is the storefront: chi routes, templ views, htmx partials. Every page is one
// or two indexed queries over the read model; images are static files.
package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"georges/internal/assets"
	"georges/internal/chat"
	"georges/internal/httpx"
	"georges/internal/store"
	"georges/internal/taxonomy"
	"georges/internal/web/views"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	st          *store.Store
	dataDir     string
	publicURL   string
	dev         bool
	r           chi.Router
	render      *renditions
	chat        *chat.Service
	chatLimiter *ipLimiter
}

// New wires the storefront. assistant may be nil or disabled; the chat UI then stays hidden.
func New(st *store.Store, dataDir, publicURL string, dev bool, assistant *chat.Service) *Server {
	s := &Server{st: st, dataDir: dataDir, publicURL: publicURL, dev: dev, chat: assistant, chatLimiter: newIPLimiter(20, 8)}
	s.render = newRenditions(dataDir, st)
	sub, _ := fs.Sub(staticFS, "static")
	views.AssetVersion = httpx.Version(staticFS, "static/app.css")[:8]
	r := httpx.Base(dev)
	r.Handle("/static/*", http.StripPrefix("/static/", httpx.Static(sub)))
	r.Get("/a/*", s.asset)
	r.Get("/files/*", s.file)
	r.Get("/img/{w}/*", s.fileResized)

	r.Get("/", s.home)
	r.Get("/shop", s.shop)
	r.Get("/shop/{room}", s.shop)
	r.Get("/search", s.shop)
	r.Get("/suggest", s.suggest)
	r.Get("/brands", s.brands)
	r.Get("/brand/{slug}", s.brand)
	r.Get("/p/{id}", s.product)
	r.Get("/sku/{sku}", s.sku)
	r.Get("/doc/{id}", s.doc)
	r.Get("/catalogs", s.catalogs)
	r.Get("/catalog/{id}", s.catalog)
	r.Get("/catalog/{id}/read", s.catalog)
	r.Get("/catalog/{id}/page/{n}", s.catalogPage)
	r.Get("/catalogs/search", s.catalogSearch)
	r.Post("/chat", s.chatHandler)
	r.Get("/rep", s.repHandler)
	r.Get("/robots.txt", s.robots)
	r.Get("/sitemap.xml", s.sitemap)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Get("/readyz", s.ready)
	r.NotFound(s.notFound)
	s.r = r
	go s.warm()
	return s
}

// warm fills the caches the first visitor would otherwise pay for (room counts, facets,
// totals and tile cards of every room, the home page's featured row) and keeps them fresh,
// so a cold page after a deploy or a cache expiry never costs the three seconds the facet
// GROUP BYs take over 600k documents.
func (s *Server) warm() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		s.st.RoomCounts(ctx)
		s.st.Browse(ctx, store.Filter{PerPage: 8})
		for _, rm := range taxonomy.Rooms {
			s.st.Browse(ctx, store.Filter{Room: rm.Slug, PerPage: 48, WithFacets: true})
			tiles := taxonomy.RoomTiles[rm.Slug]
			names := make([]string, len(tiles))
			for i, t := range tiles {
				names[i] = t.Name
			}
			s.st.TileCards(ctx, rm.Slug, names)
		}
		cancel()
		time.Sleep(20 * time.Minute) // under the 30-minute cache life
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.r.ServeHTTP(w, r) }

// page fills the layout context every template needs.
func (s *Server) page(ctx context.Context, r *http.Request, title string) views.Page {
	rooms, _ := s.st.RoomCounts(ctx)
	rep := false
	if c, err := r.Cookie("rep"); err == nil && c.Value == "1" {
		rep = true
	}
	return views.Page{Title: title, Rooms: rooms, Query: r.URL.Query().Get("q"), Dev: s.dev, Canonical: s.publicURL + r.URL.Path, Chat: s.chat != nil && s.chat.Enabled(), Rep: rep}
}

func (s *Server) html(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		slog.Error("render", "path", r.URL.Path, "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("handler", "path", r.URL.RequestURI(), "err", err)
	msg := "Something went wrong on our side. Please try again in a moment."
	if s.dev {
		msg = err.Error()
	}
	s.html(w, r, http.StatusInternalServerError, views.ErrorPage(s.page(r.Context(), r, "Error"), 500, msg))
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.html(w, r, http.StatusNotFound, views.ErrorPage(s.page(r.Context(), r, "Not found"), 404, "We couldn't find that page."))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.st.Pool.Ping(ctx); err != nil {
		http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ready"))
}

// ---- pages ----

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	covers, err := s.st.Covers(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	feat, err := s.st.Browse(ctx, store.Filter{PerPage: 8, Sort: ""})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	brands, _ := s.st.Brands(ctx)
	books, _ := s.st.Catalogs(ctx, "")
	if len(books) > 12 {
		books = books[:12]
	}
	p := s.page(ctx, r, "")
	p.Description = "George's Kitchen & Bath, Pasadena: faucets, sinks, tubs, showers, toilets, vanities, door and cabinet hardware from the brands designers specify."
	s.html(w, r, 200, views.Home(p, covers, feat.Cards, brands, books))
}

func parseFilter(r *http.Request) store.Filter {
	q := r.URL.Query()
	f := store.Filter{
		Room:       chi.URLParam(r, "room"),
		Category:   q.Get("category"),
		Brand:      q.Get("brand"),
		Tones:      q["tone"],
		Query:      strings.TrimSpace(q.Get("q")),
		Sort:       q.Get("sort"),
		ShowParts:  q.Get("parts") == "1",
		WithFacets: true,
		PerPage:    48,
	}
	if f.Room != "" && taxonomy.RoomName(f.Room) == "" {
		f.Room = ""
	}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if n, err := strconv.ParseInt(q.Get("min"), 10, 64); err == nil && n > 0 {
		f.PriceMin = n * 100
	}
	if n, err := strconv.ParseInt(q.Get("max"), 10, 64); err == nil && n > 0 {
		f.PriceMax = n * 100
	}
	return f
}

func (s *Server) shop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := parseFilter(r)
	var brand *store.Brand
	if f.Brand != "" {
		brand, _ = s.st.BrandBySlug(ctx, f.Brand)
	}
	res, err := s.st.Browse(ctx, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if res.Total == 0 && f.Query != "" {
		// a model number typed short or with the wrong punctuation: offer the nearest ones
		res.DidYouMean, _ = s.st.Suggest(ctx, f.Query, 8)
	}
	var tiles []taxonomy.Tile
	tileCards := map[string]store.Card{}
	if f.Room != "" && f.Category == "" && f.Query == "" && f.Brand == "" {
		tiles = taxonomy.RoomTiles[f.Room]
		names := make([]string, len(tiles))
		for i, t := range tiles {
			names[i] = t.Name
		}
		tileCards, _ = s.st.TileCards(ctx, f.Room, names)
	}
	title := "Shop"
	switch {
	case f.Query != "":
		title = "Search: " + f.Query
	case f.Category != "":
		title = f.Category
	case brand != nil:
		title = brand.Name
	case f.Room != "":
		title = taxonomy.RoomName(f.Room)
	}
	p := s.page(ctx, r, title)
	if r.Header.Get("HX-Request") != "" && r.Header.Get("HX-Target") == "shop" {
		s.html(w, r, 200, views.ShopBody(f, res, tiles, tileCards, brand))
		return
	}
	s.html(w, r, 200, views.Shop(p, f, res, tiles, tileCards, brand))
}

func (s *Server) brand(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	b, err := s.st.BrandBySlug(r.Context(), slug)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if b == nil {
		s.notFound(w, r)
		return
	}
	q := r.URL.Query()
	q.Set("brand", slug)
	r.URL.RawQuery = q.Encode()
	s.shop(w, r)
}

func (s *Server) brands(w http.ResponseWriter, r *http.Request) {
	bs, err := s.st.Brands(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.html(w, r, 200, views.Brands(s.page(r.Context(), r, "Brands"), bs))
}

func (s *Server) suggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := s.st.Suggest(r.Context(), q, 7)
	if err != nil {
		slog.Warn("suggest", "err", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=30")
	views.Suggest(items, q).Render(r.Context(), w)
}

func (s *Server) product(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	f, err := s.st.FamilyByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if f == nil {
		s.notFound(w, r)
		return
	}
	related, _ := s.st.Related(r.Context(), f, 8)
	p := s.page(r.Context(), r, f.Brand.Name+" "+f.Title)
	if f.Description != nil {
		p.Description = views.Truncate(*f.Description, 160)
	}
	s.html(w, r, 200, views.Product(p, f, related))
}

func (s *Server) sku(w http.ResponseWriter, r *http.Request) {
	id, err := s.st.FamilyBySKU(r.Context(), chi.URLParam(r, "sku"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if id == 0 {
		http.Redirect(w, r, "/search?q="+chi.URLParam(r, "sku"), http.StatusFound)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/p/%d", id), http.StatusMovedPermanently)
}

func (s *Server) doc(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var url string
	var p *string
	err := s.st.Pool.QueryRow(r.Context(), `SELECT url, path FROM documents WHERE id=$1`, id).Scan(&url, &p)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if p != nil {
		if abs, err := assets.Abs(s.dataDir, *p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				w.Header().Set("Content-Type", "application/pdf")
				w.Header().Set("Cache-Control", "public, max-age=604800")
				http.ServeFile(w, r, abs)
				return
			}
		}
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) catalogs(w http.ResponseWriter, r *http.Request) {
	brand := r.URL.Query().Get("brand")
	books, err := s.st.Catalogs(r.Context(), brand)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.html(w, r, 200, views.Catalogs(s.page(r.Context(), r, "Catalogs"), books, brand))
}

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	c, err := s.st.CatalogByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if c == nil {
		s.notFound(w, r)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if c.Pages != nil && page > *c.Pages {
		page = *c.Pages
	}
	toc, _ := s.st.TOC(r.Context(), id)
	title := c.Name
	if c.Title != nil {
		title = *c.Title
	}
	s.html(w, r, 200, views.Reader(s.page(r.Context(), r, title), c, page, toc))
}

func (s *Server) catalogPage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	n, _ := strconv.Atoi(chi.URLParam(r, "n"))
	rel, err := s.st.PagePath(r.Context(), id, n)
	if err != nil || rel == "" {
		http.NotFound(w, r)
		return
	}
	abs, err := assets.Abs(s.dataDir, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	httpx.ServeFileCached(w, r, abs)
}

func (s *Server) catalogSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	hits, err := s.st.SearchPages(r.Context(), q, 40)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.html(w, r, 200, views.PageHits(s.page(r.Context(), r, "Catalog search"), q, hits))
}

// ---- files ----

// fileRoots are the DATA_DIR folders /files and /img may serve. Everything else in DATA_DIR
// (Postgres, price files, logs, masters) stays unreachable.
var fileRoots = map[string]bool{"logos": true, "catalogs": true, "covers": true, "crops": true, "crops_hd": true, "webimg": true}

func cleanRel(rel string) string {
	rel = path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))[1:]
	root, _, ok := strings.Cut(rel, "/")
	if !ok || !fileRoots[root] {
		return ""
	}
	switch strings.ToLower(path.Ext(rel)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".svg":
		return rel
	}
	return ""
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	rel := cleanRel(strings.TrimPrefix(r.URL.Path, "/files/"))
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	httpx.ServeFileCached(w, r, filepath.Join(s.dataDir, filepath.FromSlash(rel)))
}

// fileResized serves /img/{w}/{path}: an on-demand WebP rendition of a mutable file (covers,
// catalog page renders, logos), cached on disk. Product pictures never come this way; they are
// assets with a pre-built ladder.
func (s *Server) fileResized(w http.ResponseWriter, r *http.Request) {
	wd, err := strconv.Atoi(chi.URLParam(r, "w"))
	if err != nil || !assets.Allowed(wd) {
		http.NotFound(w, r)
		return
	}
	rel := cleanRel(chi.URLParam(r, "*"))
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	out, err := s.render.file(rel, wd)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		slog.Warn("img", "path", rel, "err", err)
		http.Error(w, "image error", 500)
		return
	}
	httpx.ServeFileCached(w, r, out)
}

// asset serves /a/{short}/{w}.webp from the pre-built ladder. A miss in development builds
// the ladder on the spot so a fresh checkout shows pictures; in production a miss is a 404
// and Caddy normally serves these files directly from disk anyway.
func (s *Server) asset(w http.ResponseWriter, r *http.Request) {
	short, wd, ok := assets.ParseURL(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.dataDir, filepath.FromSlash(assets.RenditionPath(short, wd)))
	if _, err := os.Stat(p); err != nil {
		if !s.dev {
			http.NotFound(w, r)
			return
		}
		if err := s.render.asset(r.Context(), short); err != nil {
			slog.Warn("asset build on demand", "hash", short, "err", err)
			http.NotFound(w, r)
			return
		}
	}
	httpx.ServeFileImmutable(w, r, p)
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /suggest\nDisallow: /search\nDisallow: /chat\nSitemap: %s/sitemap.xml\n", s.publicURL)
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, u := range []string{"/", "/shop/bathroom", "/shop/kitchen", "/shop/hardware", "/shop/appliances", "/brands", "/catalogs"} {
		fmt.Fprintf(w, "<url><loc>%s%s</loc></url>", s.publicURL, u)
	}
	rows, err := s.st.Pool.Query(r.Context(), `SELECT family_id FROM search_docs WHERE grade <= 1 AND tier <> 2 ORDER BY family_id LIMIT 45000`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				fmt.Fprintf(w, "<url><loc>%s/p/%d</loc></url>", s.publicURL, id)
			}
		}
	}
	fmt.Fprint(w, `</urlset>`)
}
