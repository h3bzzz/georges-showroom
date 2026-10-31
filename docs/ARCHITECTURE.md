# Architecture — rebuilding George's in Go

Goal: replace the Unilog CX1 storefront + SPEX tooling with our own stack, fed by our own
catalog database, with **much better product imagery** (harvested from vendor catalogs and spec
sheets) and an AI assistant/visual-search layer trained on every product we carry.

## 1. Data acquisition (the "crawler" is really three pipelines)

| Pipeline | Source | Transport | Volume | Package |
|---|---|---|---|---|
| **A. Price file** | `ftp.myplumbingshowroom.com` daily feed (brand, SKU, UPC, prices) | FTP | ~2.85M rows/day | `jlaffaye/ftp`, `encoding/csv` |
| **B. Product pages** | `www.georgesshowroom.com/Pasadena-California/*.HTM` via sitemap | real Chrome (Cloudflare) | 2.85M SKU pages, but only **~1 per family** needs full parse (~1M) | `go-rod/rod` + `go-rod/stealth`, `PuerkitoBio/goquery` |
| **C. Documents** | `documents.cx1accel.com` spec/install PDFs; `catalogs.myplumbingshowroom.com` flipbooks | plain HTTP | 329 catalogs / 34k pages; ~500k PDFs | `net/http` + `hashicorp/go-retryablehttp`, `x/time/rate`, `pdfcpu`, `pdftotext`/`tesseract` CLIs |

Why this split: A gives us the complete SKU universe cheaply every day, so B only has to crawl
**deltas and one representative page per family** (attributes, description, documents,
collection, related). C never touches Cloudflare at all and can run at full speed.

Cloudflare strategy for B: rod launches the installed `google-chrome-stable` **headed** with a
persistent profile dir (`.chrome-profile/`). You log in once (site login from `.env` can be
automated after the CF cookie exists). Sitemap XML files are fetched inside that same session.
Keep concurrency ≤ 2 tabs and ≥ 500 ms between navigations; robots asks for a 5 s crawl delay
for bots — we are an authenticated site admin pulling our own data, but stay polite.

All three pipelines run as **River** jobs (Postgres-backed queue, `riverqueue/river`) so they
are resumable, rate-limited per queue, and observable from one table.

## 2. Storage

PostgreSQL 16 (+ `pgvector`, `pg_trgm`). Schema in `internal/store/migrations`:

- `brands` (code, slug, name) · `collections`
- `product_families` (brand, family_key, title, description, category path, attributes JSONB)
- `products` (SKU level: sku, upc, finish, family_id, online_product_id, url, list/site/trade
  price, lead-time/freight fields for later) · `price_history`
- `product_images` (source: site|catalog|spec, url, local path, w×h, phash, embedding vector)
- `documents` (spec/install PDF, text) · `catalogs` / `catalog_pages` (page image, text, OCR)
- `crawl_state` (url, etag/ver, last_seen) for delta crawling
- `projects`, `project_items`, `quotes` — our SPEX replacement

Access layer: `jackc/pgx/v5` + `sqlc` generated queries, `pressly/goose` migrations.
Objects (images/PDFs) on local disk now, S3-compatible (MinIO) later via `minio-go`.

## 3. Serving

- HTTP: `go-chi/chi/v5`; templates: `a-h/templ`; interactivity: htmx; CSS: Tailwind.
- Search: Postgres FTS + trigram first (fast to ship); swap to Meilisearch when facets get heavy.
- Image pipeline: `h2non/bimg` (libvips) for resize/webp; serve via `/img/{sha}/{w}.webp`.
- Auth: sessions in Postgres (`alexedwards/scs`), roles: customer / rep / admin.
- Rep tools: projects, rooms, item selection, quote PDF (`go-pdf/fpdf` or headless Chrome print).

## 4. AI layer (Python sidecar, Go orchestrates)

- **Retrieval first, fine-tuning second.** Index every product with text embeddings
  (description + attributes + catalog page text) and image embeddings (SigLIP/CLIP over product
  images and catalog crops) in pgvector. "Photo on the floor → 3–5 closest items" is a
  nearest-neighbour query, no training needed to get a strong v1.
- Fine-tune later: contrastive fine-tune of the image tower on (catalog image ↔ SKU) pairs we
  harvest; a small instruct model (e.g. Qwen/Llama class from Hugging Face) fine-tuned or
  RAG-served over the product corpus for cost/lead-time/freight Q&A.
- Sidecar: FastAPI + `transformers` exposing `/embed/text`, `/embed/image`; Go calls it and
  writes vectors. Training scripts live in `ml/`.

## 5. Repo layout

```
cmd/georges/            CLI (cobra): catalogs, specs, pricefile, browser, sitemap, db, serve
internal/config         .env → typed config
internal/browser        rod session w/ persistent Chrome profile, CF-aware fetch
internal/sitemap        fetch + parse zgeorges-SiteIndex*.xml (through browser)
internal/site           parsers for product / listing pages (goquery)
internal/catalogs       FlipHTML5 client: probe, page images, text
internal/specs          cx1accel PDF fetch + pdftotext
internal/pricefile      FTP list/download + CSV → rows
internal/store          pgx, migrations, sqlc
internal/jobs           River workers
internal/web            chi + templ handlers
ml/                     Python sidecar + training
data/                   seed tables (brands.tsv)
docs/                   RESEARCH.md, this file
```

## 6. Build order
1. `catalogs` + `specs` fetchers (no Cloudflare, testable today) → local corpus of images/text.
2. `pricefile` FTP ingest → `products` table populated daily.
3. `browser` + `sitemap` + `site` parsers → families, attributes, documents, images.
4. Web storefront over our DB; then rep project tools.
5. Embedding sidecar + visual search; then fine-tuning.
