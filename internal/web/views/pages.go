package views

import "georges/internal/store"

func catalogTitle(c store.Catalog) string {
	if c.Title != nil && *c.Title != "" {
		return *c.Title
	}
	return c.Name
}
