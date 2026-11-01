-- +goose Up
-- photo_tone is the tone of the picture a family's card shows (its vendor master's SKU finish,
-- else the vision verdict on its crop), kept here so a finish query can rank 3,000 families
-- without a correlated lookup per row (3.6 s → 50 ms). Set by RefreshFamilyTones.
ALTER TABLE family_best ADD COLUMN IF NOT EXISTS photo_tone text;
-- +goose Down
ALTER TABLE family_best DROP COLUMN IF EXISTS photo_tone;
