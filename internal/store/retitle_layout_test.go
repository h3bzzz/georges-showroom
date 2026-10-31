package store

import "testing"

func TestHeadingTextNotePrefix(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"COMPATIBLE WITH TROUGH SINKS:", "", false},
		{"For use with Kubista Sinks", "", false},
		{"Drain sold separately", "", false},
		{"Aquagrande Sinks", "Aquagrande Sinks", true},
	}
	for _, c := range cases {
		got, ok := headingText(c.in, "Lacava")
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("headingText(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestLayoutTitleGroupPrefix(t *testing.T) {
	lines := []pageLine{
		{Text: "COMPATIBLE WITH TROUGH SINKS:", X0: 600, X1: 1100, Y: 560, H: 28},
		{Text: "SINKTOP - K60B-FLT", X0: 600, X1: 900, Y: 610, H: 22},
		{Text: "SINK - 5260", X0: 600, X1: 800, Y: 780, H: 22},
	}
	got, sc := layoutTitle(lines, 700, 780, "Lacava")
	if got != "Trough Sinks" || sc < 15 {
		t.Errorf("layoutTitle = %q (%d), want Trough Sinks", got, sc)
	}
}
