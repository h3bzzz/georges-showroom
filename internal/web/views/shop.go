package views

import (
	"strconv"

	"georges/internal/store"
)

func i64(v int64) string { return strconv.FormatInt(v, 10) }

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func tileLQIP(cards map[string]store.Card, name string) *int {
	if c, ok := cards[name]; ok {
		return c.LQIP
	}
	return nil
}

// pageWindow returns page numbers to show with 0 as an ellipsis.
func pageWindow(cur, total int) []int {
	if total <= 9 {
		out := make([]int, 0, total)
		for i := 1; i <= total; i++ {
			out = append(out, i)
		}
		return out
	}
	var out []int
	add := func(n int) {
		if len(out) > 0 && out[len(out)-1] == n {
			return
		}
		out = append(out, n)
	}
	add(1)
	lo, hi := max(2, cur-2), min(total-1, cur+2)
	if lo > 2 {
		add(0)
	}
	for i := lo; i <= hi; i++ {
		add(i)
	}
	if hi < total-1 {
		add(0)
	}
	add(total)
	return out
}
