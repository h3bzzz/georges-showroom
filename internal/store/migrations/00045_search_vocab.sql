-- +goose Up
-- Words the catalog knows (titles, collections, categories, finishes, brands) with how often
-- they occur; a query word that is not here is spelled against it with pg_trgm
-- ("brused nickle" → "brushed nickel"). Rebuilt by RefreshVocab (ingest categories/best).
CREATE TABLE IF NOT EXISTS search_vocab (
  word text PRIMARY KEY,
  n    int NOT NULL
);
CREATE INDEX IF NOT EXISTS search_vocab_trgm ON search_vocab USING gin (word gin_trgm_ops);
-- +goose Down
DROP TABLE IF EXISTS search_vocab;
