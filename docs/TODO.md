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

## 2026-10-31 imaging + pricing accuracy pass (branch `imaging-pricing`)

**Pricing gold set.** Site products carry both George's price and the maker's list, and their
ratio is constant per brand: Emtek 0.80 (3,668 SKUs), Watermark/California Faucets/most 0.75,
Baldwin/Newport/Icera 0.70, Top Knobs 0.90 → `brands.price_multiplier` is now that observed
ratio (migration 00035; `ingest prices` refreshes it). Catalog-vs-site list prices (1,400
SKU pairs over BAL/CAL/ETK, matched on finish-stripped SKUs) agree to within one price-book
year: median ratio CAL 1.00, ETK 1.56 (2026 book vs 2024 site), BAL 1.36; no systematic
parse error survived except Baldwin's "Boulder (85354) … $82" cross-reference lines.
Systematic OCR errors found and fixed in `internal/ocr` (tests in index_test.go, sku_test.go):
dropped decimal points on cents-style pages ("$22800", "$4,93500", "$12995" — Chicago,
Aboutwater, Alno, Ashley Norton, Barber Wilsons; page- and book-level style detection),
parenthesised finish codes/cross-references taken as SKUs ("(US10B)" became a $240 Emtek
product, 415 lines), junk SKUs ("4.5L/MIN", "A112.1016/ASSE", "120Z", "2700K/4000K",
phone numbers; `ocr.JunkSKU`, purged by `catalogs items`), Lacava "SINK - 5260" labelled
models. Fence: ×40 around the brand median (was ×20; Ashley Norton's $1,600 pulls and
Chicago's $12 aerators are real), parts cap = max($7,500, 10× brand median) (Barber
Wilsons' $12,000 mixers sit in "Bathtub Parts"). Result after `catalogs items` +
`ingest prices --reset`: catalog products priced 22,454 → 22,582, quarantined 322 → 28
(Chicago 169 → 3, Alno 46 → 0, Classic Brass 22 → 4, Ashley Norton 22 → 0); 76 junk-SKU
products removed. Remaining 28 flags are mostly genuine cheap parts under 1/40 of the median
(Hydro Systems surcharges, Classic Brass spindles) and three Baldwin ×51 reads.

**Imaging audit.** 353-family stratified sample of card photos (brand × photo_how) judged by
eye: 26 wrong (7.4 %), of which 10 are Lacava finish-swatch tables and dimensioned vanity
renders in a book that had no vision verdict yet, 4 are lifestyle/mood-board openers, 3 are
dimensioned drawings, 2 a display board; excluding the unclassified Lacava crops 4.8 %.
Rules changed in `RefreshFamilyBest`: a crop the model calls text/other at ≥ 0.6 is never a
card; a series opener must read as a photograph; parts/rough-ins/drains (tier 2 or a parts
category, `series_ok()`, migration 00037) never borrow the collection shot. A milder veto
(text/other 0.4, subject "other" 0.5) was tried and reverted: on 40 removed cards it was
wrong half the time (Duravit vanities, Gessi spouts and flush plates read as "other").
New subject classes lighting/electrical with 23 Buster + Punch labels so pendants and switch
plates stop reading as "other" (needs `catalogs vision --reclassify`). Emtek's 5.7k crops had
never been audited or embedded (`catalogs audit/vision --brand ETK` run).

**Spec images.** `product_images` source='spec' rows get the same CLIP verdict (migration
00036, `catalogs vision --spec`: 565 images → 329 photo, 200 drawing, 17 text); a spec photo
at ≥ 0.8 whose subject fits replaces a crop under 300 px or of quality < 0.3 and fills
families with no picture, below a vendor master (`photo_how='spec'`): 56 families gained a
card (Watermark 38, which has no catalog at all, California Faucets 9, Emtek 7); all 40
sampled spec cards are product photographs.

**Card counts.** Before: 30,694 families, 15,182 with a card photo (12,195 crop, 2,987
series) + 3,041 vendor masters. After (with the books the catalog track ingested meanwhile):
34,054 families, 15,816 with a card photo (11,651 crop, 4,109 series, 56 spec) + 3,041
masters; zero cards carry a crop the model calls text/other at ≥ 0.6. Still open: 335
catalog products no price-book line supports any more (Emtek finish codes "US10B" created
before the reference rule) and the Lacava swatch grids until `catalogs vision --brand LAV`
finishes; the subject head's "other" class is too soft to veto below 0.7.

**Duplicate shots.** `catalogs dedupe` (migration 00041, `catalog_crops.dup_of`) marks a crop
that repeats a larger copy of the same photograph within a brand (CLIP cosine ≥ 0.97 on the
stored embeddings, aspect within 15 %, largest kept): 17,011 of 50.8k crops (California
Faucets 2,811, Baldwin 2,601, Ashley Norton 2,382); 19 of 20 sampled pairs were the same
shot. The product gallery and the upscaler queue skip duplicates. PDQ (ajdnik/imghash) is
still the answer for crops without an embedding; none exist once `catalogs vision` has run.

