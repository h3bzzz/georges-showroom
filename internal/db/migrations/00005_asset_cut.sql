-- +goose Up
-- cut: how much of the object runs off the frame (0 whole object … 1 cropped through the
-- middle). A catalog crop that stops at a column boundary cuts the faucet in half; such a
-- picture is never a card and sits last in a gallery.
ALTER TABLE assets ADD COLUMN cut real;
-- +goose Down
ALTER TABLE assets DROP COLUMN cut;
