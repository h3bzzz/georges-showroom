# Search & browse design (2026-10-17)

How mature catalogs do it (Wayfair Query Intent Engine / Query Classifier, Algolia Query
Categorization, Elastic multi-tier retrieval and multiplicative boosts, DoorDash MUST/SHOULD
attributes, Constructor attribute enrichment, Etsy relevance labels) and what we run.

Sources: aboutwayfair.com (ML + NLP search; query classifier), algolia.com/doc (query
categorization; optional filters), elastic.co/search-labs (multi-tier ecommerce governance;
multiplicative boosting; combined_fields = BM25F), postgresql.org/docs/16 (textsearch-controls,
textsearch-features ts_rewrite, pgtrgm), constructor.com (attribute enrichment).

## Principles

1. **Taxonomy first.** A product type is a curated field, a title is noisy text. Category
   pages filter on the category; queries that name a type retrieve by category and only fall
   back to text when that finds too little. *(done: `internal/store/taxonomy.go`,
   `BrowseFilter.StrictType`, category groups for tiles)*
2. **Filter when confident, boost otherwise.** *(done, crudely: strict when ≥ 8 category hits;
   otherwise text retrieval with a +6 category bonus in `Intent.relevance`)*. Next: per-term
   confidence from a `query_type_lexicon` table and click logs.
3. **Tiered retrieval, stop at a full page.** *(done: strict type → all concepts over the blob
   → finish widened → any concept)*
4. **Demote parts and accessories unless asked for.** *(done: `family_tier` × 4 in relevance,
   `WantsParts()` exemption; Brizo SKU prefixes HL/HK/HX/RP/R/T force part categories)*.
   Next: make it multiplicative once scoring moves to `ts_rank_cd`.
5. **Relationship phrases are not names.** "for use with wall-mounted faucets", "compatible
   with", "sold separately" → never a title *(done: `reNoteLine`, `endsDangling` in items.go)*.
   Next: keep them as `compatible_with` text at D weight.
6. **Synthesized display name** = brand + collection + mounting + type + finish when the OCR
   title is junk ("Graff Sento Single-Hole Lavatory Faucet"). *Next: needs collection and
   mounting attributes for catalog families.*
7. **One weighted search document.** A = display name + type + type synonyms; B = brand,
   collection, model, mounting, holes; C = raw title, finishes; D = description/notes.
   `ts_rank_cd('{0.05,0.2,0.5,1.0}', doc, q, 32)`; synonyms at query time with `ts_rewrite`
   over an `aliases` table (no reindex). *Next; replaces the trigram blob for ranking, the blob
   stays as the fuzzy last tier.*
8. **Attribute intent:** geometry is MUST (widespread / 8", single-hole / monobloc, 4"
   centerset, GPM, ADA), finish is SHOULD (OCR finishes are unreliable) *(partly: finish families
   are soft already; mounting/holes extraction next)*.
9. **Per-type facet sets:** faucets → Mounting, Holes, Handles, Finish, Flow rate, Spout
   height/reach, ADA, Collection, Brand, Price; toilets → Rough-in, Bowl shape, GPF, Height.
10. **Golden query set:** 100 labelled queries, CI asserts precision@10.

## Synonym groups to seed

lav / lavatory / bathroom sink faucet / basin mixer · widespread / 8-inch / 3-hole · centerset /
4-inch · single-hole / monobloc / monoblock · mini-widespread / minispread · rough / rough-in /
valve body · trim / trim kit / escutcheon kit · tub filler / roman tub / deck-mount tub faucet ·
handshower / hand shower / handheld · showerhead / shower head / rain head · pressure balance /
PB · thermostatic / thermo · pop-up / drain assembly · toilet / water closet / WC · GPM / flow
rate · GPF / flush volume · ADA / accessible · matte black / MB · polished chrome / CP · brushed
nickel / BN / satin nickel.

## Part / accessory demotion terms

cartridge, aerator, escutcheon, flange, valve body, rough-in kit, extension kit, handle kit,
hose, mounting kit, gasket, o-ring, supply line, stop, p-trap, tailpiece, stopper, faucet deck,
deck plate, spec sheet, cut sheet, installation, parts diagram, replacement, repair kit,
"for use with", "fits", "compatible with".


## Finish resolution (2026-11-01)

"antique rubbed bronze" is not a finish anyone stocks; Oil Rubbed Bronze, Antique Bronze and
Venetian Bronze are. The query therefore resolves to real finish names before it touches the
database — the canonical-value-plus-synonyms pattern (Bloomreach `color_group`, Google
Retail `ColorInfo.color_families`, Algolia/Constructor attribute enrichment) applied to
finishes, plus Bloomreach's "colour-based image SKU selection" for the picture.

- `internal/store/finishdict.go`: every distinct `products.finish` is classified once into
  tones (bronze, nickel, brass…), a head tone (Black Nickel reads black, Nickel Silver reads
  silver, slash two-tones match either), a treatment (polished | satin | matte | antique |
  unlacquered | unspecified) and canonical tokens; brand modifiers (PVD, Lifetime, Vibrant,
  Luxe) are dropped. Cached five minutes from the products table, no extra table to maintain.
- `intent.go` `finishRun`: a run of finish words anchored by a tone or treatment word is one
  concept; group words expand (silver → chrome, nickel, stainless, pewter; gold → gold, brass).
  `FinishDict.Resolve` returns every name that shares the tone and does not contradict the
  treatment (Dark Bronze is in, Satin Bronze is out), with Exact = names whose canonical tokens
  equal the query's (Jaccard 1.0): "antique rubbed bronze" → Oil Rubbed Bronze, Venetian
  Bronze, Antique Bronze, Distressed ORB… + the unspecified bronzes.
- Retrieval is exact: `f.id IN (SELECT family_id FROM products WHERE finish = ANY(names))`,
  answered by `products(finish, family_id)` in 20 ms. Matching the names in the family blob
  as well cost 900 ms (a 24-way regex defeats the trigram index) and only added families whose
  finish is unknown — those are the `finish:any` widening tier, used when a page is not full.
- Ranking (`browse.go` `rankFamilies`, a CTE over a slim projection, then hydration of 24
  rows): stocked in the finish (3) and type/category hits; parts (tier 2) last; families with
  a photo; then **off-tone pictures behind every round** (fit −1: the card's picture shows
  another metal, read from `family_best.photo_tone` — CLIP verdict, else the SKU finish of
  the crop); then a brand round-robin with one slot per brand per round for finish queries
  and a repeated title waiting three rounds; inside a brand's slot the picture decides (vendor
  master in the finish 2 > crop in the tone 1 > unknown 0), then the exact name, then tier.
  On a finish page the picture outranks the tier: a bronze-pictured towel ring beats a
  chrome-pictured faucet. 5.3 s → 0.25 s for the whole page; `RANK_DEBUG=1` prints the keys.
- Picture: `preferFinishImages` swaps the card to a vendor master in the finish, a crop linked
  to a SKU in it, or a crop in the right tone; cards that still show another metal say
  "pictured in chrome / nickel". Tones come from `catalogs vision --tone`: zero-shot CLIP
  prompts mixed with prototypes from 1,740 crops linked to SKUs of a known finish, scored from
  the stored embeddings (no image work), written to `catalog_crops.vtone`; vendor images take
  the tone of their SKU's finish.
- Chips under the count ("Just one finish: Oil Rubbed Bronze · Venetian Bronze …") narrow to
  one exact finish via the existing facet.

Next: a `finish_aliases` table for brand-specific words the classifier cannot guess (Brizo
"Luxe Steel" is stainless, Newport "Forever Brass"), the same resolution for the header
search's suggestions, and a golden query set asserting precision@24 per finish family.
