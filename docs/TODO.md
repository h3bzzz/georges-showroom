# TODO

- [x] **Research the best Go packages for each subsystem** — newest, most stable, best
      optimized. Cover: browser automation (rod/chromedp/playwright-go), HTML parsing, HTTP
      client + retries + rate limiting, FTP, PDF (pdfcpu, unipdf, poppler bindings), OCR
      (gosseract vs tesseract CLI vs PaddleOCR sidecar), image processing (bimg/govips/imaging),
      perceptual hashing, DB (pgx/sqlc/goose/tern), job queue (river/asynq/gocraft), web
      (chi/echo/fiber + templ + htmx/datastar), search (Postgres FTS, Bleve, Meilisearch,
      Typesense), object storage, auth/sessions, PDF generation for quotes, observability
      (slog, otel). Record versions, last release dates, maintenance signals, and a
      recommendation in `docs/PACKAGES.md`.
- [ ] Copy FTP credentials from the site's Price File Access page into `.env`, then run
      `georges pricefile ls` and document the file layout/columns.
- [ ] **User step:** `georges browser login` once (headed; clears Cloudflare, persists `.chrome-profile`). Then
      `georges sitemap fetch` (57 files → crawl_queue) and `nohup georges crawl run > data/raw/crawl.log &`
      (2026-10-31: resumable queue in `crawl_queue`, one page per family, finish-dropdown siblings marked
      covered, challenge back-off; `crawl stats`). Expect ~1 page/s ⇒ ~300k family pages ≈ 4 days.
- [x] Product page parser (goquery) → families, SKUs, attributes, documents, images.
- [x] Catalog metadata, covers and text layers synced (`georges catalogs sync`).
- [~] `georges catalogs pipeline` running: download → OCR → extract product photos for all 324 books (hours; resumable; log in data/raw/pipeline.log).
- [x] Postgres (embedded for dev) + goose migrations. [ ] River workers.
- [x] Storefront v0 (chi + templ + htmx: home, brands, brand, products, product, search, catalogs). [ ] SPEX-style project tools.
- [~] CLIP in-process (hugot + ONNX Runtime, `internal/vision`, 2026-10-17): every crop embedded once
      (`catalogs vision`, ≈2 h CPU for 44k), classified medium (photo | detail | drawing | spec | text) and
      subject (faucet | handle | sink | …) by a class-balanced linear probe on `data/vision_labels.tsv`
      (508 hand labels; 10-fold 89 % / 64 %). `crop_kind()` + `subject_fits()` (migration 00029) decide
      card photos. Next: more labels via `catalogs sheet` (detail recall is weakest), then the stored
      embeddings become the "photo → closest products" index (brute-force cosine in Go; no pgvector in
      the embedded PG bundle).
- [x] Product imagery comes from catalog crops only (`catalog_crops`, `/sku/{sku}`); old-site images are never rendered.
- [~] `georges upscale` (Real-ESRGAN x4, ncnn-vulkan on the Intel iGPU, ~8 s/crop) running over the
      13.7k family photos under 500 px, then drawings; log data/raw/upscale.log. Flipbook pages
      max out at 1700x2200 so crops are inherently small; sharper sources = spec-sheet PDFs (cx1accel)
      and manufacturer sites (`webimages`, needs BRAVE_API_KEY).
- [x] 2026-10-03 review: `/files/*` no longer serves DATA_DIR raw (Postgres dir, price file, logs were reachable);
      `internal/imgsrv` serves allow-listed images + `/img/{w}/…` cached renditions with srcset on cards; admin routes
      loopback-only; `serve` starts the embedded Postgres itself (air works from cold); upscaler feeds PNG re-cuts, not JPEG crops.
- [~] `georges vendorimages`: Brizo masters (2000px white PNG by SKU) → product_images web → cards. Next vendors: Elkay
      Scene7 (asset id), Delta/Hansgrohe via PDP scrape; trade logins (Kohler Pro, Duravit pro, Hansgrohe pro, Moen) — user.
- [x] 2026-10-17 pricing rework: `internal/pricing` (strict `ParseCents`, split "$" and bare-dollar forms, median/MAD
      fence per brand), `ocr.IsIndexPage` (numerical indexes read "186,193" as $186,193 → the $139k Duravit toilet),
      `georges ingest prices [--audit|--reset]`, `products.price_flag` quarantine ("Call for price" instead of a wrong
      number), `catalogs items`/pipeline quarantine automatically.
- [ ] Price precedence still to wire: site price > FTP price file (cost, list, lead time) > catalog list × brand
      multiplier. `brands.price_multiplier` is 0.75 for every brand; set real per-brand multipliers from the price file.
- [x] 2026-10-17 search: fixtures (tier 0) outrank parts (tier 2) unless the query names a part; brand round-robin
      tightened to 2 per round; Brizo glued finish codes (RP71650NK) merge into one family and fill `products.finish`.
- [x] 2026-10-17 `ingest categories`: title-rule categories for catalog families (sidebar facets now span all brands);
      `catalogs audit` tags thick-stroke dimension sketches as drawings so cards never show spec art.
- [x] 2026-10-17 card photos: page-links only on single-product pages, no crops <110 px, `catalogs tidy` trims
      slivers/captions, Brizo SKU-prefix categories (handle kits are parts), dangling-sentence titles rejected.
- [ ] Retitle the 15k catalog families whose titles are still junk ("(continued)", "Model No. Model No.", spec rows):
      they get no category and sink to tier 1. Then re-run `ingest categories --force`.
- [x] 2026-10-17 taxonomy-first retrieval (docs/SEARCH.md): tiles and type queries retrieve by category, text only as
      fallback; category bonus in relevance; note-lines ("for use with…") can no longer be titles.
- [~] 2026-10-31 `georges catalogs info` (`internal/catinfo`, migration 00040 `family_facts`): catalog pages → typed
      facts (spout height/reach, W×H×D, centers, flow gpm/gpf, mounting, holes, handles, material, certifications,
      cartridge, drain, door-hardware function/handing, designer, works-with), collection (printed TOC > page heading),
      feature bullets, printed finishes → products.finish. Site attributes win; catalog families get all keys.
      Next: per-type facet filters from family_facts; Ashley Norton/Emtek column tables (CTC/length/projection);
      Gessi/BWC finish-group legends; spec-sheet (`documents.text`) facts for site families.
- [ ] Synthesized display names (brand + collection + mounting + type + finish) for junk-titled catalog families;
      `query_type_lexicon` confidence table; per-type facet sets; golden query set (docs/SEARCH.md 2, 6, 9, 10).
- [ ] Weighted tsvector (title+finish A, type B, spec D) with `ts_rank_cd`; see docs/PACKAGES.md 2026-10-17.
- [ ] `catalogs items` SKU sanity filter: Brizo products include OCR junk ("1016/CSA", "4.5L/MIN", "A112.1016/ASSE").
- [ ] Upscaler model A/B on 20 crops: realesrgan-x4plus vs ncnn conversions of 4xNomos8kSC / remacri (see memory
      georges-imaging-research-2026); pick per `kind` (photo vs drawing).
- [~] Spec sheets: `georges specs sync` (2026-10-31) rewrites the old pages' `georges.Requested-Info.com` links (403) to
      `documents.cx1accel.com`, stores PDF + pdftotext text + page count, extracts embedded rasters ≥300 px with
      `pdfimages` → product_images source='spec' (shown last in the product gallery, never on cards). `/doc/{id}`
      serves the local copy. Next: pdftoppm 300 DPI renders of vector dimension drawings; vision-classify spec
      images so true product photos can reach cards; attribute extraction from `documents.text`.
- [ ] WebP renditions in imgsrv (gen2brain/webp, pure Go) negotiated on Accept; thumbhash placeholders per crop.
- [ ] PDQ hash (ajdnik/imghash) on crops to collapse duplicate collection shots across books before upscaling.
- [ ] Embedding sidecar: onnxruntime_go + DINOv2-B → pgvector HNSW; photo-in → 5 nearest families.
- [ ] Improve crop→SKU linking per brand (SKU patterns, multi-column layouts, finish tables); rank crops by size/quality.
- [ ] Price file: parse columns → products table (blocked on FTP_PASS).
