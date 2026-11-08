# Architecture — George's v2 (branch `v2`, 2026-11-07)

v2 is a restart from zero after the first deploy. v1 stays on `main` as the raw-data
pipeline (crawler, catalog OCR, crops, vision) writing the `georges` database; v2 owns the
`georges2` database and the site. Module path: `georges`.

## Flow

```
v1 database (georges)        import legacy      v2 database (georges2)
  product_families  ───────────────────────▶  families, items, documents, catalogs
  catalog_crops, product_images ───────────▶  assets (hash, measures, vision verdict, embedding)
  family_crops ────────────────────────────▶  asset_links (the evidence)

media pick      asset_links × assets × taxonomy  ─▶  family_media (card / gallery / drawing)
assets build    every asset: trim, measure, LQIP, PDQ, WebP ladder  ─▶  data/raw/assets/…
index build     families + items + family_media  ─▶  search_docs (one row per family)
serve           chi + templ + htmx over search_docs / families / family_media
```

## Imaging

- **Asset** = immutable picture identified by sha256. Source file stays where it is
  (`src_path`); the ladder `320/640/960/1600.webp` lives at `assets/ab/cd/<hash16>/`, URL
  `/a/<hash16>/<w>.webp`, cached immutable. Caddy serves the ladder from disk in production;
  the app only builds a missing rendition in development.
- **Measures** (`internal/assets`): white-border trim with a 3 % pad (studio framing like
  Ferguson's `c_lpad`), `bg_white`, `fill`, Laplacian `sharpness`, Lean LQIP integer (CSS-only
  placeholder, 8 bytes of HTML per card), pHash + PDQ (near-duplicate detection), composite
  `quality`. The vision verdict (CLIP medium/subject/tone/scene) and embedding carry over from
  v1 until the SigLIP 2 swap.
- **Picker** (`internal/media`): pure Go with tests. Provenance (vendor master > spec raster >
  SKU-linked crop > near > page) then picture quality; vetoes for swatches, lifestyle, detail
  shots, wrong subject, slivers, tiny crops, busy pages; grade 0/1/2 drives browse order.
  Cross-family fallbacks (collection opener, same-title twin) run in SQL after the direct pass.

## Search

`search_docs` carries title, brand, category, groups, tones, finishes, SKUs, price, the card
asset and a weighted tsvector (title + SKUs A, collection + brand B, category C, finishes +
facts D) plus a trigram blob. The intent parser (`internal/search`) reads brand, type words,
finish phrase and model numbers before the index is touched. Default order: tier, picture
grade, precomputed brand round-robin, popularity.

## Web

No CDN, no framework: vendored htmx 2.0.11, one hand-written stylesheet, a 2 KB script.
Cards reserve a square, lazy-load below the fold, `content-visibility: auto`. Pages render in
one or two indexed queries (browse ≈ 50–150 ms, product ≈ 20 ms on the workstation).

## Commands

```
bin/georges db start | migrate | sql | exec | stats
bin/georges import legacy [--only brands,catalogs,families,items,documents,skus,assets,links,covers]
bin/georges media pick
bin/georges assets build [--shown] [--source vendor|catalog|spec] [--limit N]
bin/georges index build
bin/georges serve [--addr :8080]      # air runs this
```
