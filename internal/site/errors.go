package site

import "errors"

var (
	ErrNotProduct = errors.New("site: page is not a product page")
	ErrNotListing = errors.New("site: page is not a listing page")
	ErrChallenge  = errors.New("site: page is a bot challenge (Cloudflare or iAmHuman)")
)
