# TODO (v2)

Done 2026-11-07: v2 skeleton, schema, importer, asset core + ladder, media picker, search
index, storefront (home, shop, product, brands, catalogs, reader, typeahead), deploy files.

- [x] `assets build` for every shown asset, `assets measure`, `media pick`, `index build` (2026-11-07).
- [ ] Spec sheets: `specs fetch` covers CAL, CHI, NEW, KOL, KLL, LAV, BAL, BRZ (Brizo sits behind Imperva: the fetcher pauses once, then stops the brand). Still needed: Robern (series in the path), Toto, Infinity Drain, Hydro Systems, Native Trails (page scrape); Emtek, Watermark, Duravit, Rohl, Graff, Dornbracht (JavaScript portals: headless browser pass).
- [ ] Deploy v2 to the VPS: push branch, workflow builds `ghcr.io/h3bzzz/georges-showroom`, rsync `data/raw/assets`, pg_dump `georges2`; compose now serves `/a/*` from Caddy.
- [x] `georges vendors fetch --brand CAL|BAL|WMK`: per-finish masters keyed to the SKU (2026-11-07).
- [ ] Vendors next: Emtek via Wagtail page JSON (agent mapping SKU→finish image into data/raw/webimg/ETK/_finish_images.json), Brizo re-run when the Imperva gate lifts, Elkay Scene7, Hansgrohe Celum, Robern Scene7.
- [ ] Finish renderings (user asked 2026-11-07): metal mask (BiRefNet) + per-finish transform learned from real pairs (same-treatment pairs reach 35 dB; cross-treatment needs re-lighting), always labelled "Rendered in X", replaced by the real photo when one lands; living-finish patina slider later.
- [ ] SigLIP 2 base ONNX in Go (hugot): re-embed every asset, text→image search, photo→SKU matcher (`/match` endpoint) — the chatbot's eyes.
- [ ] Chatbot: Claude API with tools over search_docs/items/documents + the matcher; rep mode with cost/lead time once the price file is parsed.
- [ ] Display titles for junk-titled catalog families (brand + collection + type + mounting).
- [ ] Cut-outs for crops with busy backgrounds (BiRefNet_lite / BEN2 ONNX, offline).
- [ ] Super-resolution A/B (SPAN / Compact) for crops under 400 px; drawings rendered from PDFs at 300 DPI instead.
- [ ] Price file ingest (FTP) → items.price_cents / lead time; per-brand multipliers.
- [ ] Image QA loop: contact sheets of random pages, `assets reject`, re-pick.
- [ ] Projects / quotes (SPEX parity): wish list → room-assigned PDF with spec sheets.
