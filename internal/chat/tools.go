package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"georges/internal/assets"
	"georges/internal/store"
	"georges/internal/taxonomy"
	"georges/internal/web/views"
)

// tool builds a strict tool definition.
func tool(name, desc string, props map[string]any, required ...string) anthropic.ToolUnionParam {
	if required == nil {
		required = []string{}
	}
	t := anthropic.ToolParam{
		Name:        name,
		Description: anthropic.String(desc),
		Strict:      anthropic.Bool(true),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties:  props,
			Required:    required,
			ExtraFields: map[string]any{"additionalProperties": false},
		},
	}
	return anthropic.ToolUnionParam{OfTool: &t}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func nullable(m map[string]any) map[string]any {
	m["type"] = []string{m["type"].(string), "null"}
	return m
}

// tools lists what the assistant may call. Strict schemas require every property to be
// listed in required, so optional inputs are nullable.
func (s *Service) tools(rep bool) []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		tool("search_products",
			"Search the showroom catalog. Returns up to `limit` product families with id, title, brand, category, price range, finishes and a link. Use natural language in `query` (type of fixture, finish, style, mounting); set room/brand/tones/price when the visitor said them. Model numbers also work in `query`.",
			map[string]any{
				"query":     str("What to look for, e.g. 'wall mount tub filler', 'pull-down kitchen faucet', or a model number. Empty string to browse by filters only."),
				"room":      nullable(str("bathroom | kitchen | hardware | appliances")),
				"category":  nullable(str("A category name as shown on the site, e.g. 'Kitchen Faucets', 'Bathroom Sinks', 'Shower Heads', 'Toilets', 'Cabinet Pulls'.")),
				"brand":     nullable(str("Brand name or slug, e.g. 'Brizo', 'california-faucets'.")),
				"tone":      nullable(str("Finish tone: silver | gold | bronze | black | white | copper | nickel")),
				"min_price": nullable(num("Minimum price in whole dollars.")),
				"max_price": nullable(num("Maximum price in whole dollars.")),
				"limit":     num("How many results to return, 1–8."),
			}, "query", "room", "category", "brand", "tone", "min_price", "max_price", "limit"),
		tool("get_product",
			"Full detail for one product family: description, specifications, every SKU with its finish and price"+ifRep(rep, ", list price, UPC and lead time", "")+", spec sheets and the catalog pages it is printed on. Pass the family id from a search or a transcript, or a SKU/model number.",
			map[string]any{
				"id":  nullable(num("Family id from search_products or the transcript.")),
				"sku": nullable(str("A SKU or model number when no id is known.")),
			}, "id", "sku"),
		tool("search_catalog_pages",
			"Full-text search over the OCR text of the manufacturers' catalogs and price books (dimensions, rough-in notes, collection descriptions). Returns catalog, page, snippet and a link to the page. Use when a question needs installation or technical facts the product record lacks.",
			map[string]any{
				"query": str("Words to find, e.g. 'rough-in valve 1/2 inch Rohl' or a model number."),
				"limit": num("Results to return, 1–8."),
			}, "query", "limit"),
		tool("list_brands",
			"The brands the showroom carries with how many products each has, optionally for one room.",
			map[string]any{
				"room": nullable(str("bathroom | kitchen | hardware | appliances")),
			}, "room"),
		tool("similar_products",
			"Products whose photograph looks most like a given product's photograph (visual similarity across all brands). Good for 'something like this but cheaper/in another brand'.",
			map[string]any{
				"id":    num("Family id."),
				"limit": num("Results to return, 1–8."),
			}, "id", "limit"),
	}
}

func ifRep(rep bool, a, b string) string {
	if rep {
		return a
	}
	return b
}

// callTool runs one tool and returns the model's result, cards for the browser and a status line.
func (s *Service) callTool(ctx context.Context, name string, input []byte, rep bool) (toolOut, []Card, string) {
	switch name {
	case "search_products":
		var in struct {
			Query    string  `json:"query"`
			Room     *string `json:"room"`
			Category *string `json:"category"`
			Brand    *string `json:"brand"`
			Tone     *string `json:"tone"`
			MinPrice *int64  `json:"min_price"`
			MaxPrice *int64  `json:"max_price"`
			Limit    int     `json:"limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return toolOut{err.Error(), true}, nil, ""
		}
		return s.searchProducts(ctx, in.Query, deref(in.Room), deref(in.Category), deref(in.Brand), deref(in.Tone), derefI(in.MinPrice), derefI(in.MaxPrice), in.Limit)
	case "get_product":
		var in struct {
			ID  *int64  `json:"id"`
			SKU *string `json:"sku"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return toolOut{err.Error(), true}, nil, ""
		}
		return s.getProduct(ctx, derefI(in.ID), deref(in.SKU), rep)
	case "search_catalog_pages":
		var in struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return toolOut{err.Error(), true}, nil, ""
		}
		return s.searchPages(ctx, in.Query, in.Limit)
	case "list_brands":
		var in struct {
			Room *string `json:"room"`
		}
		_ = json.Unmarshal(input, &in)
		return s.listBrands(ctx, deref(in.Room))
	case "similar_products":
		var in struct {
			ID    int64 `json:"id"`
			Limit int   `json:"limit"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return toolOut{err.Error(), true}, nil, ""
		}
		return s.similar(ctx, in.ID, in.Limit)
	}
	return toolOut{"unknown tool " + name, true}, nil, ""
}

func clampLimit(n int) int {
	if n < 1 {
		return 5
	}
	if n > 8 {
		return 8
	}
	return n
}

func (s *Service) searchProducts(ctx context.Context, q, room, category, brand, tone string, minP, maxP int64, limit int) (toolOut, []Card, string) {
	f := store.Filter{Query: strings.TrimSpace(q), Room: strings.ToLower(room), Category: category, PerPage: clampLimit(limit), PriceMin: minP * 100, PriceMax: maxP * 100}
	if f.Room != "" && taxonomy.RoomName(f.Room) == "" {
		f.Room = ""
	}
	if brand != "" {
		f.Brand = s.brandSlug(ctx, brand)
		if f.Brand == "" {
			return toolOut{fmt.Sprintf("unknown brand %q; call list_brands for the names we carry", brand), true}, nil, ""
		}
	}
	if tone != "" {
		groups := taxonomy.ToneGroups([]string{strings.ToLower(tone)})
		if len(groups) == 0 {
			groups = []string{strings.ToLower(tone)}
		}
		f.Tones = groups
	}
	status := "Searching"
	if f.Query != "" {
		status += " for " + f.Query
	} else if category != "" {
		status += " " + category
	}
	status += "…"
	res, err := s.Store.Browse(ctx, f)
	if err != nil {
		return toolOut{err.Error(), true}, nil, status
	}
	// a query with a filter that empties it: retry on words alone so the model can say what
	// was relaxed instead of "nothing"
	relaxed := ""
	if res.Total == 0 && f.Query != "" && (f.Category != "" || len(f.Tones) > 0 || f.Brand != "") {
		g := store.Filter{Query: f.Query, Room: f.Room, PerPage: f.PerPage}
		if r2, err := s.Store.Browse(ctx, g); err == nil && r2.Total > 0 {
			res, relaxed = r2, "filters (category/brand/tone) dropped because they matched nothing"
		}
	}
	cards := make([]Card, 0, len(res.Cards))
	type row struct {
		ID       int64    `json:"id"`
		Title    string   `json:"title"`
		Brand    string   `json:"brand"`
		Category string   `json:"category,omitempty"`
		Price    string   `json:"price,omitempty"`
		Finishes []string `json:"finishes,omitempty"`
		URL      string   `json:"url"`
	}
	rows := make([]row, 0, len(res.Cards))
	for _, c := range res.Cards {
		card := toCard(c)
		cards = append(cards, card)
		rows = append(rows, row{card.ID, card.Title, card.Brand, card.Category, card.Price, c.Finishes, card.URL})
	}
	out := map[string]any{"total_matches": res.Total, "results": rows}
	if relaxed != "" {
		out["note"] = relaxed
	}
	if res.Total == 0 {
		out["note"] = "nothing matched; try broader words, another category name, or list_brands"
	}
	return jsonOut(out), cards, status
}

func toCard(c store.Card) Card {
	card := Card{ID: c.FamilyID, Title: c.Title, Brand: c.Brand, URL: fmt.Sprintf("/p/%d", c.FamilyID), LQIP: c.LQIP}
	if c.Category != nil {
		card.Category = *c.Category
	}
	if c.PriceCents != nil && *c.PriceCents > 0 {
		card.Price = views.Money(*c.PriceCents)
	}
	if c.Hash != nil {
		card.Image = assets.URL(*c.Hash, 640)
	}
	if len(c.Finishes) > 6 {
		card.Finishes = c.Finishes[:6]
	} else {
		card.Finishes = c.Finishes
	}
	return card
}

func (s *Service) brandSlug(ctx context.Context, name string) string {
	bs, err := s.Store.Brands(ctx)
	if err != nil {
		return ""
	}
	n := norm(name)
	for _, b := range bs {
		if norm(b.Slug) == n || norm(b.Name) == n {
			return b.Slug
		}
	}
	for _, b := range bs { // prefix: "newport" → newport-brass
		if strings.HasPrefix(norm(b.Name), n) || strings.HasPrefix(norm(b.Slug), n) {
			return b.Slug
		}
	}
	return ""
}

var reNorm = regexp.MustCompile(`[^a-z0-9]`)

func norm(s string) string { return reNorm.ReplaceAllString(strings.ToLower(s), "") }

func (s *Service) getProduct(ctx context.Context, id int64, sku string, rep bool) (toolOut, []Card, string) {
	if id == 0 && sku != "" {
		fid, err := s.Store.FamilyBySKU(ctx, sku)
		if err != nil {
			return toolOut{err.Error(), true}, nil, ""
		}
		if fid == 0 {
			// fall back to search so a partial model number still lands
			res, err := s.Store.Browse(ctx, store.Filter{Query: sku, PerPage: 1})
			if err == nil && len(res.Cards) > 0 {
				fid = res.Cards[0].FamilyID
			}
		}
		if fid == 0 {
			return toolOut{fmt.Sprintf("no product carries SKU %q", sku), true}, nil, ""
		}
		id = fid
	}
	if id == 0 {
		return toolOut{"pass an id or a sku", true}, nil, ""
	}
	f, err := s.Store.FamilyByID(ctx, id)
	if err != nil {
		return toolOut{err.Error(), true}, nil, ""
	}
	if f == nil {
		return toolOut{fmt.Sprintf("no product with id %d", id), true}, nil, ""
	}
	status := "Opening " + f.Brand.Name + " " + f.Title + "…"
	type item struct {
		SKU    string `json:"sku"`
		Finish string `json:"finish,omitempty"`
		Price  string `json:"price,omitempty"`
		List   string `json:"list_price,omitempty"`
		UPC    string `json:"upc,omitempty"`
		Lead   string `json:"lead_time,omitempty"`
		Photo  bool   `json:"has_finish_photo,omitempty"`
	}
	items := make([]item, 0, len(f.Items))
	for i, it := range f.Items {
		if i >= 40 {
			break
		}
		x := item{SKU: it.SKU, Finish: deref(it.Finish), Photo: it.Hash != nil}
		if it.PriceCents != nil && *it.PriceCents > 0 {
			x.Price = views.Money(*it.PriceCents)
		} else if it.PriceFlag != nil {
			x.Price = "on request"
		}
		if rep {
			if it.ListCents != nil && *it.ListCents > 0 {
				x.List = views.Money(*it.ListCents)
			}
			x.UPC = deref(it.UPC)
			if it.LeadDays != nil {
				x.Lead = fmt.Sprintf("%d days", *it.LeadDays)
			}
		}
		items = append(items, x)
	}
	facts := map[string]string{}
	for _, fa := range f.Facts {
		v := fa.Value
		if fa.Unit != "" {
			v += " " + fa.Unit
		}
		facts[fa.Key] = v
	}
	for k, v := range f.Attributes {
		if _, dup := facts[k]; !dup && len(facts) < 40 {
			facts[k] = v
		}
	}
	type doc struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	docs := make([]doc, 0, len(f.Documents))
	for _, d := range f.Documents {
		docs = append(docs, doc{d.Kind, fmt.Sprintf("/doc/%d", d.ID)})
	}
	type pg struct {
		Catalog string `json:"catalog"`
		Page    int    `json:"page"`
		URL     string `json:"url"`
	}
	pages := make([]pg, 0, len(f.Pages))
	for _, p := range f.Pages {
		pages = append(pages, pg{p.Title, p.Page, fmt.Sprintf("/catalog/%d?page=%d", p.CatalogID, p.Page)})
	}
	bullets := f.Bullets
	if len(bullets) > 8 {
		bullets = bullets[:8]
	}
	out := map[string]any{
		"id": f.ID, "title": f.Title, "brand": f.Brand.Name, "collection": deref(f.Collection), "category": deref(f.Category), "room": deref(f.Room),
		"url": fmt.Sprintf("/p/%d", f.ID), "description": views.Truncate(deref(f.Description), 700), "bullets": bullets, "specifications": facts,
		"finishes": f.Finishes, "skus": items, "documents": docs, "catalog_pages": pages,
	}
	if f.Tier == 2 {
		out["note"] = "this is a part or rough-in valve, not a finished fixture"
	}
	var cards []Card
	if len(f.Media) > 0 {
		c := Card{ID: f.ID, Title: f.Title, Brand: f.Brand.Name, Category: deref(f.Category), URL: fmt.Sprintf("/p/%d", f.ID), Image: assets.URL(f.Media[0].Hash, 640), LQIP: f.Media[0].LQIP}
		if lo, _ := priceRange(f.Items); lo > 0 {
			c.Price = views.Money(lo)
		}
		cards = append(cards, c)
	}
	return jsonOut(out), cards, status
}

func priceRange(items []store.Item) (lo, hi int64) {
	for _, it := range items {
		if it.PriceCents == nil || *it.PriceCents <= 0 {
			continue
		}
		if lo == 0 || *it.PriceCents < lo {
			lo = *it.PriceCents
		}
		if *it.PriceCents > hi {
			hi = *it.PriceCents
		}
	}
	return
}

var reTags = regexp.MustCompile(`</?b>`)

func (s *Service) searchPages(ctx context.Context, q string, limit int) (toolOut, []Card, string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return toolOut{"query is empty", true}, nil, ""
	}
	hits, err := s.Store.SearchPages(ctx, q, clampLimit(limit))
	if err != nil {
		return toolOut{err.Error(), true}, nil, ""
	}
	type hit struct {
		Catalog string `json:"catalog"`
		Brand   string `json:"brand,omitempty"`
		Page    int    `json:"page"`
		Snippet string `json:"snippet"`
		URL     string `json:"url"`
	}
	out := make([]hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, hit{h.Catalog, deref(h.Brand), h.Page, reTags.ReplaceAllString(h.Snippet, ""), fmt.Sprintf("/catalog/%d?page=%d", h.CatalogID, h.Page)})
	}
	return jsonOut(map[string]any{"hits": out}), nil, "Searching the catalogs for " + q + "…"
}

func (s *Service) listBrands(ctx context.Context, room string) (toolOut, []Card, string) {
	bs, err := s.Store.Brands(ctx)
	if err != nil {
		return toolOut{err.Error(), true}, nil, ""
	}
	type br struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Products int    `json:"products"`
		Room     string `json:"room,omitempty"`
		URL      string `json:"url"`
	}
	var out []br
	for _, b := range bs {
		if b.Families == 0 {
			continue
		}
		if room != "" && (b.Room == nil || !strings.EqualFold(*b.Room, room)) {
			continue
		}
		out = append(out, br{b.Name, b.Slug, b.Families, deref(b.Room), "/brand/" + b.Slug})
	}
	return jsonOut(map[string]any{"brands": out}), nil, "Listing brands…"
}

func (s *Service) similar(ctx context.Context, id int64, limit int) (toolOut, []Card, string) {
	if s.Matcher == nil {
		return toolOut{"visual similarity is not available on this server; use search_products with the product's category and finish instead", true}, nil, ""
	}
	ms, err := s.Matcher.Similar(ctx, id, clampLimit(limit))
	if err != nil {
		return toolOut{err.Error(), true}, nil, ""
	}
	if len(ms) == 0 {
		return toolOut{"no picture to compare for that product", true}, nil, ""
	}
	cards, txt := s.matchCards(ctx, ms)
	return toolOut{text: txt}, cards, "Finding look-alikes…"
}

// cardsByID loads cards in the given order.
func (s *Service) cardsByID(ctx context.Context, ids []int64) ([]Card, error) {
	rows, err := s.Store.Pool.Query(ctx, `
		SELECT sd.family_id, sd.brand, sd.title, sd.category, sd.price_cents, sd.card_hash, sd.card_lqip, sd.finishes
		FROM search_docs sd JOIN unnest($1::bigint[]) WITH ORDINALITY AS u(id, ord) ON u.id = sd.family_id
		ORDER BY u.ord`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		var c store.Card
		if err := rows.Scan(&c.FamilyID, &c.Brand, &c.Title, &c.Category, &c.PriceCents, &c.Hash, &c.LQIP, &c.Finishes); err != nil {
			return nil, err
		}
		out = append(out, toCard(c))
	}
	return out, rows.Err()
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefI(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// Tool runs one tool by name for debugging from the command line (no model involved).
func (s *Service) Tool(ctx context.Context, name, input string, rep bool) (string, []Card, bool) {
	out, cards, _ := s.callTool(ctx, name, []byte(input), rep)
	return out.text, cards, out.err
}
