# Go package selection (researched 2026-09-26)

Method: every candidate was resolved against the Go module proxy (`go list -m -json pkg@latest`)
for its latest tag and release date, then weighed for maintenance, cgo burden, and fit.
Versions below are the latest as of 2026-09-26. Local toolchain: Go 1.27.1, libvips 8.18.6,
tesseract 5.5.3, poppler 26.08, Node 24 (for Tailwind only), no Docker.

## Decisions

| Subsystem | Choice | Version (date) | Why | Runner-up |
|---|---|---|---|---|
| CLI | `spf13/cobra` | v1.10.2 (2025-12) | de-facto standard, stable | `urfave/cli/v3` |
| Config | `caarlos0/env/v11` + `joho/godotenv` | v11.4.1 (2026-05) | struct tags, zero ceremony | `knadh/koanf/v2` v2.3.7 if we add YAML |
| Logging | `log/slog` + `lmittmann/tint` | v1.2.0 (2026-07) | stdlib, pretty dev output | — |
| Browser (Cloudflare) | `go-rod/rod` + `go-rod/stealth` | v0.116.2 (2024-07) / v0.4.9 (2023-05) | highest-level CDP API, auto-wait, persistent profile; what matters for Cloudflare is a *real headed Chrome profile*, which both drivers give | `chromedp/chromedp` v0.16.0 (2026-07) — more actively released; switch if rod stalls. `playwright-go` v0.6201.1 needs the Node driver, rejected |
| HTML parsing | `PuerkitoBio/goquery` | v1.13.0 (2026-08) | jQuery-style selectors over `x/net/html` | `gocolly/colly/v2` (can't pass Cloudflare, rejected) |
| HTTP fetch (no-CF hosts) | `hashicorp/go-retryablehttp` + `x/time/rate` + `x/sync/errgroup` | v0.7.8 / v0.16.0 / v0.23.0 | retries with backoff, token-bucket, bounded parallelism | `imroc/req/v3` v3.61.0 (TLS fingerprinting; only if a CDN starts blocking) |
| FTP | `jlaffaye/ftp` | v0.2.4 (2026-08) | the maintained Go FTP client, explicit TLS support | — |
| PDF (spec sheets) | `gen2brain/go-fitz` (MuPDF) | v1.28.2 (2026-07) | text **and** page rasterization in one lib, static-linked, no external binary | `pdfcpu/pdfcpu` v0.15.0 (2026-08) for object ops (split/merge/extract images); `pdftotext` CLI as fallback |
| OCR (catalogs w/o text layer) | `tesseract` 5.5.3 via `os/exec` | — | avoids cgo build pain; `gosseract/v2` is stale (2023-09) | PaddleOCR/`hugot` ONNX later if accuracy matters on price tables |
| Image processing | `davidbyttow/govips/v2` (libvips) | v2.19.0 (2026-09) | 4–8× faster than pure Go, webp/avif, generated bindings; libvips 8.18 present | `h2non/bimg` v1.1.9 (2022, stale); `disintegration/imaging` (2019, stale); `x/image` v0.46.0 for pure-Go bits |
| Perceptual hash (dedupe) | `corona10/goimagehash` | v1.1.0 (2022-05) | small, stable, pHash/dHash | `ajdnik/imghash` v1.1.0 (2026-02) |
| Database driver | `jackc/pgx/v5` | v5.11.0 (2026-09) | fastest Postgres driver, native types, COPY | — |
| Query codegen | `sqlc-dev/sqlc` | v1.31.1 (2026-04) | type-safe SQL → Go, pgx/v5 target | `jackc/tern` for migrations (chosen goose below) |
| Migrations | `pressly/goose/v3` | v3.28.0 (2026-09) | SQL files, embeddable, up/down | `golang-migrate/migrate/v4` v4.20.1 |
| Vectors | `pgvector/pgvector-go` | v0.4.1 (2026-07) | pgx codec for `vector` | — |
| Job queue | `riverqueue/river` | v0.47.0 (2026-08) | Postgres-native, transactional enqueue, generics, web UI | `hibiken/asynq` v0.26.0 (needs Redis) |
| HTTP router | `go-chi/chi/v5` | v5.3.2 (2026-08) | `net/http` native, composable middleware; + `go-chi/httprate` v0.16.0, `go-chi/httplog/v3` v3.5.0 | `labstack/echo/v4` v4.15.4; `gofiber/fiber/v3` v3.5.0 (fasthttp, non-standard, rejected) |
| Templates | `a-h/templ` | v0.3.1020 (2026-05) | typed, compiled HTML components | `html/template` |
| Interactivity | **htmx** (user decision) | 2.x CDN/vendored | request/response fragments, simplest mental model | Datastar (`starfederation/datastar-go` v1.2.2) if we later need SSE streaming |
| CSS | Tailwind v4 via `npx @tailwindcss/cli` | — | build-time only, no Node at runtime | — |
| Sessions/auth | `alexedwards/scs/v2` | v2.9.0 (2025-04) | Postgres-backed sessions, simple | `coreos/go-oidc/v3` v3.21.0 if we add Google SSO for reps |
| CSRF | `gorilla/csrf` | v1.7.3 (2025-01) | works with htmx headers | — |
| Validation | `go-playground/validator/v10` | v10.30.5 (2026-09) | forms/API | — |
| Search | Postgres FTS + `pg_trgm` now; **Meilisearch** (`meilisearch-go` v0.36.3) when typo-tolerance/instant search is needed | — | keep one datastore until search UX demands more | `blevesearch/bleve/v2` v2.6.1 (pure Go, single-node) |
| Object storage | local disk now; `minio/minio-go/v7` v7.3.0 or `aws-sdk-go-v2/s3` v1.113.4 later | — | | |
| Quote/spec-book PDFs | headless Chrome print (rod) from templ pages | — | one rendering path for web and PDF | `johnfercher/maroto/v2` v2.4.2 (2026-09), `signintech/gopdf` v0.38.1 for programmatic layouts |
| Cache | `dgraph-io/ristretto/v2` | v2.4.2 (2026-07) | in-process cache for hot product/brand lookups | `maypok86/otter` |
| IDs | `oklog/ulid/v2` | v2.1.2 (2026-07) | sortable ids for projects/quotes | `google/uuid` v1.6.0 |
| ML inference in Go | `knights-analytics/hugot` (ONNX Runtime pipelines) | v0.8.0 (2026-09) | run SigLIP/CLIP/text embedders **in Go**, no Python at serve time | `yalue/onnxruntime_go` v1.36.0 (lower level). Python (HF `transformers`) stays for **training** only |
| LLM API | `anthropics/anthropic-sdk-go` | v1.75.0 (2026-09) | rep assistant / spec Q&A over retrieved products | — |
| Observability | `go.opentelemetry.io/otel` | v1.46.0 (2026-08) | traces for crawl + web | — |
| Dev tooling | `air-verse/air` v1.67.4, `golangci-lint/v2` v2.14.0, `stretchr/testify` v1.12.1 | | | `testcontainers-go` v0.44.0 needs Docker (absent) — use `fergusstrange/embedded-postgres` v1.34.0 for tests |

## Notes
- **Stale but acceptable:** rod (2024) and goimagehash (2022) are feature-complete and widely
  used; both have active issue trackers. Re-evaluate rod vs chromedp at the next milestone.
- **cgo inventory:** govips (libvips), go-fitz (bundled MuPDF static libs), hugot (onnxruntime
  shared lib). Everything else is pure Go. Docker images will need `libvips` + `onnxruntime`.
- **Not chosen on purpose:** fiber (fasthttp incompatibility), colly (no Cloudflare), gosseract
  (stale + cgo), imaging/bimg (unmaintained), asynq (extra Redis), testcontainers (no Docker).

## Re-check 2026-10-17 (pricing, search, imaging)

Versions re-resolved against the module proxy on 2026-10-17. One hard fact changes two rows
above: the embedded Postgres bundle (`data/raw/pg/bin`, PG 16.9) ships only `pg_trgm` and
`unaccent`; `vector` is **not** available and migration `00002_vectors.sql` silently took its
no-pgvector branch. Any embedding work needs pgvector built against that `pg_config` or a move
to system/embedded PG 18.

| Area | Module | Version (date) | Verdict |
|---|---|---|---|
| Money | `internal/pricing` (ours) | — | cents stay `bigint`; strict `ParseCents` + MAD fence replace the old regex. `shopspring/decimal` v1.5.0 (2026-10) only if we ever need fractional-cent math (cost × multiplier) |
| Outliers | `montanaflynn/stats` v0.13.0 (2026-10) | — | not adopted: the median/MAD fence is 40 lines in `internal/pricing/fence.go` |
| Search | Postgres FTS `setweight` + `ts_rank_cd` + `pg_trgm` | in PG 16.9 | **next step**: title+finish = A, type/collection = B, spec text = D so "brushed nickel" no longer surfaces rough valves via spec tables. `pg_search` (ParadeDB, AGPL) and `pg_textsearch` (PG17+) cannot load into the zonky binaries |
| Search sidecar | `blevesearch/bleve/v2` v2.6.1 (2026-08) | Apache-2 | consider only if SQL ranking plateaus (BM25 opt-in, per-field boost, facets) |
| Tokenising / stemming | `clipperhouse/uax29/v2` v2.7.0, `kljensen/snowball` v0.10.0, `agnivade/levenshtein` v1.2.2 | MIT | adopt when intent.go grows typo tolerance; `blevesearch/segment` is stale |
| libvips | `cshum/vipsgen` v1.3.11 (2026-09) | MIT | adopt (generated from libvips 8.18); `govips` v2.20.0 acceptable; pick one |
| ONNX / embeddings | `knights-analytics/hugot` v0.8.3 (2026-10, go1.27) | Apache-2 | **adopted** (`internal/vision`, build with `-tags ORT`; ORT 1.31.0 + Xenova CLIP ViT-B/16 auto-downloaded under data/raw/tools). Two workarounds: the Xenova vision tower needs `ImageFeatureExtractionPipeline` with the `image_embeds` output selected by index, and a text model without an `attention_mask` input embeds only BOS unless `GoTokenizer.AttentionMask` is forced on (hugot sizes input_ids from the mask). Its built-in zero-shot image pipeline re-runs the vision tower once per label, so embed once and score in Go instead |
| Perceptual hash | `ajdnik/imghash/v2` v2.5.2 (2026-06) | MIT | adopt: PDQ + DINOHash for duplicate collection shots; replaces `corona10/goimagehash` |
| PDF | `pdfcpu/pdfcpu` v0.16.2 (2026-10) + `klippa-app/go-pdfium` v1.21.3 (wazero) | Apache-2 / MIT | adopt for spec-sheet image extraction and 300 DPI drawing renders; `go-fitz` is AGPL (MuPDF), drop it from the table above |
| Jobs | `riverqueue/river` v0.49.0 (2026-10, go1.26 min) | MPL-2 | adopt, pin the minor (0.48 changed migrations, 0.49 tx helpers) |
| Cache | `maypok86/otter/v2` v2.3.0 | Apache-2 | adopt over ristretto (W-TinyLFU, loader + refresh) |
| Rate limit | `go-chi/httprate` v0.16.1 | MIT | adopt for inbound; `x/time/rate` stays for outbound |
| Validation / logging | `go-playground/validator/v10` v10.30.5, `lmittmann/tint` v1.2.1, `go-chi/httplog/v3` v3.5.0 | MIT | adopt when request parsing grows |
| Tests | `testcontainers-go` | — | skip (no Docker/podman on the box); `peterldowns/pgtestdb` v0.1.1 for per-test template clones on the embedded server |

Drift since 2026-09-26: chi v5.3.2→v5.5.0, templ v0.3.1020→v0.3.1070, goose v3.28.0→v3.29.0,
chromedp v0.16.0→v0.21.0 (rod still v0.116.2, untagged since 2024-07).
