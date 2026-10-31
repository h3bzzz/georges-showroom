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
