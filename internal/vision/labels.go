package vision

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Label is one hand-labelled crop: path relative to DATA_DIR, medium and subject class.
type Label struct {
	Path, Medium, Subject string
}

// LoadLabels reads a TSV of "path<TAB>medium<TAB>subject"; # lines are comments.
func LoadLabels(path string) ([]Label, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Label
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			return nil, fmt.Errorf("%s:%d: want path<TAB>medium<TAB>subject", path, n)
		}
		out = append(out, Label{Path: cols[0], Medium: cols[1], Subject: cols[2]})
	}
	return out, sc.Err()
}
