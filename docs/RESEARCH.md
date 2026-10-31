# georgesshowroom.com — Site Anatomy (researched 2026-09-26)

## Platform
- **Unilog CX1 Showroom** (formerly "myplumbingshowroom.com" / CIMM2 family). Template `B41`,
  meta `mpsServer=Java-CF1` → Java + **ColdFusion** (`*.cfm` endpoints). Front end: Bootstrap 4.4,
  jQuery 3.3, MegaNavbar, Revolution Slider, MagicZoom, SmartSearchPro 1.4.1 (autocomplete).
- **Cloudflare managed challenge** on every request from non-browser clients (curl, WebFetch = 403
  "Just a moment"). Real Chrome with a persisted profile passes. All fetches of
  `www.georgesshowroom.com` must go through a real browser session (see ARCHITECTURE.md).
- Analytics: GA4 (several IDs), Universal Analytics, Google Ads, Microsoft Clarity, accessiBe.
- Site ID inside Unilog: **631** ("Georges"). Catalog published: **2,851,468 products**
  (SKU level), last data refresh 2026-09-22.
- Our login (hmoreira@) is an **ADMIN**-level site user: dashboard tabs Administration,
  Customer User Management, Company Information, Reporting, Online Orders, Project Pay,
  Brand Management, Vendor Catalogs, Unilog HyperScale (empty), CX1 Showroom Admin.

## URL grammar (all server-rendered HTML, no JSON API found)
| Kind | Pattern | Notes |
|---|---|---|
| Home | `/` | |
| Brands index | `/brands.HTML` | 141 brands (see `data/brands.tsv`) |
| Brand landing | `/<Brand-slug>-v1.htm` | facets: Category, Color, Collection; 24/page |
| Brand + category | `/<Brand-slug>-<Category-slug>-v14.htm` | e.g. `Gessi-Kitchen-faucets-v14.htm` |
| Top category | `/Bathroom-v2.htm`, `/Kitchen-v2.htm`, `/Hardware-v0.htm`, `/Appliances-vh.htm` | |
| Sub category | `/Bathroom-sink-faucets-v4.htm`, `/Bathroom-Showers-Hand-showers-v234.htm`, `/Hardware-Cabinet-Knobs-v034.htm` … | suffix `-vN` encodes menu level |
| Search | `/Search-v0.htm?searchbar=<q>` | facets Specials/Type/Category/Brand |
| Pagination | `?page=N` | 24 per page fixed; "Page N of M" text in body |
| Facets | `?finishStandard=Chrome`, `&collection=…`, `&sort=…`, `priceFrom/priceTo` (forms) | |
| **Product (SKU)** | `/Pasadena-California/<Brand>-<SKU>-<Finish>-<Category>.HTM` | one page per SKU/finish |
| Product image | `/<BRANDCODE>-<SKU-or-family>.JPEG?ver=YYMMDDNN` | 1000×1000 typical; also `-LARGE.JPEG` variants |
| Brand logo | `/<BRANDCODE>-<Brand-Name>.Gif` | brand code = 3 letters (GES, CAL, WMK…) |
| Finish swatches | `/finishGroups/<color>.jpg` | |
| Wishlist/cart | `/wishList.htm`, `/shoppingCart.htm` | |
| Quote | `/contact.HTML?AOQ=<id>` | |
| Sitemap | `/zgeorges-SiteIndex.xml` → `zgeorges-SiteIndex0..56.xml` | index 0 = brand pages (126), 1..56 ≈ 50k product URLs + image URLs each ⇒ **~2.8M product URLs, ~300k image URLs** |
| robots | crawl-delay 5 for Google/Bing; `/admin/`, `/login.HTML` disallowed | |

Listing pages show **product families** (one card, "N Finish Options"); product pages are
**SKU-level** (each finish is its own page, with a finish dropdown). Family counts:
Bathroom 8,215 pages, Kitchen 1,058, Hardware 29,022, Appliances 226 (×24 ≈ 925k family cards,
with overlap between categories).

## Product page contents (example Gessi 54112-726, online product id 4691916)
- h1 = `<Brand> <family description>`; SKU; price + "Originally $X - Save 25%" (site pricing vs MSRP);
  admin sees price level toggle (Default / Trade).
- "Product Details" block: description bullets; `Brand: SKU`; Finish; Collection; UPC;
  Online Product ID; typed attributes (Height, Mounting, ADA, Spout Reach, Weight, Valve Included…);
  Shipping dims + weight.
- Documents: `https://documents.cx1accel.com/brands/<CODE>/documents/specSheets/<file>.pdf` and
  `/installSheets/<file>.pdf` — **plain HTTP, no Cloudflare**, text-extractable (pdftotext works).
  File names are per-brand conventions (`in_art_54112.pdf`, `K30-123-XX_sp.pdf`), so harvest
  from the product page rather than guessing.
- Sections: `#collection` (same-collection products), `#relatedProducts`, `#on-display`,
  brand catalogs links (`catalogs.myplumbingshowroom.com/<CODE>/<name>`).
- No JSON-LD / microdata.
- Per-product admin tools (Information / Edit) exist for overrides.

## Vendor catalogs (the good images + prices)
- Admin page `/admin/local/listCatalogs.cfm` lists **329 catalogs, ~34,100 pages** across
  104 brand codes (price books, catalogs, brochures, sell sheets).
- Each is a **FlipHTML5** flipbook at `https://catalogs.myplumbingshowroom.com/<CODE>/<name>/`
  (no Cloudflare, plain HTTP):
  - `javascript/config.js` → `totalPagesCaption:"597"`, `pageNumberCaption`, `CreatedTime`.
  - `files/large/N.jpg` (≈2200×1700, 200–500 KB) and `files/page/N.jpg` — full page renders.
  - `files/thumb/N.jpg` thumbnails.
  - `files/search/book_config.js` → `textForPages[]` — per-page extracted text **only for some
    catalogs** (Gessi has it; California Faucets and Brizo are empty ⇒ need OCR for those).
  - Directory listing is disabled; enumerate pages 1..N.
- Public "View Vendor Catalogs" page: `/listCatalogBrands.HTML`.

## Company price file (the SKU master feed)
- `/Price-File-Access.HTML` (admin dashboard → Brand Management → "Click here for instructions")
  documents a **daily FTP feed** (≈4 a.m. EST) on `ftp.myplumbingshowroom.com`, containing
  brand, SKU, UPC, and pricing columns for every product; split into ≤1M-row files.
  Credentials are printed on that page — copy them into `.env` (`FTP_USER`, `FTP_PASS`);
  they are intentionally not stored in this repo.
- Brand Management tab: per-brand product counts, visibility, currency, vendor account #,
  sales rep, price multiplier, data/price update dates (DataTables, Excel export button).
- Reports (`/admin/reports/reportOutput.cfm?reportName=…`): `siteSetupReport`, `vendorPDF`
  (available brand list), `downloadXlsReport` (company overrides), `siteUsers`;
  `/admin/reports/WLMonthlyReport.cfm` (monthly project extracts).

## SPEX Builder / Projects (what sales reps use)
- `/products/wishlistFunction.cfm?action=controlPanel` — Project Panel: project name, location,
  employee contact (15 reps), customer address, logos, locked/prices-locked flags, accept/reject
  workflow, expiration/promise/follow-up dates, reference #, status (New → Open → Pending →
  Waiting on Customer → Approved → Ordered → Closed Won/Lost), public/private notes.
- `?action=wishListSiteAdmin` all projects, `?action=siteProductsList` custom products,
  `/admin/SPEX/userSpexSettings.cfm` + `siteSpexSettings.cfm` (document formatting defaults),
  Import Quote, XLS Load, Open By ID. Output = branded PDF selection books with spec sheets.
- Admin: `/admin/local/siteCustomData.cfm` (bulk vendor overrides),
  `relatedProductsLoad.cfm`, `siteBrandList.cfm`, `adminFunctions.cfm?action=listUsers`,
  `/admin/data/listMapHolidayBrands.cfm?siteID=631` (MAP holidays).
- Support portal: support.cx1showroom.unilogcorp.com (Freshdesk, login required for most).

## Legacy
- Wayback shows a pre-2016 WordPress/WooCommerce site (`/catalog/page/...`, `?product_categories=`).
  Ignore.

## Numbers that drive design
| Item | Count |
|---|---|
| SKU product pages | ~2.85M |
| Product image URLs in sitemap | ~300k |
| Brands | 141 (104 with catalogs) |
| Vendor catalogs / pages | 329 / ~34,100 |
| Spec + install PDFs | ≤2 per family (hundreds of thousands) |
| Sales reps | 15 |
