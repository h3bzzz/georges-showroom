// Package ocr wraps the tesseract CLI (5.x) and returns words with pixel boxes.
package ocr

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Word struct {
	Text       string
	X, Y, W, H int
	Conf       float64
	Block      int
	Par        int
	Line       int
}

type Result struct {
	Words []Word
	Text  string
}

// Run OCRs an image file. psm 3 = fully automatic page segmentation (good for catalogs).
func Run(ctx context.Context, path string) (*Result, error) {
	cmd := exec.CommandContext(ctx, "tesseract", path, "stdout", "--psm", "3", "-l", "eng", "tsv")
	// tesseract spawns OpenMP threads per process; with several processes in parallel that
	// thrashes the CPU (load 20+ on 4 cores). One thread per process is much faster overall.
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tesseract %s: %w: %s", path, err, strings.TrimSpace(errb.String()))
	}
	return parseTSV(out.String())
}

func parseTSV(s string) (*Result, error) {
	res := &Result{}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var text strings.Builder
	lastLine := -1
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue // header
		}
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 12 {
			continue
		}
		level, _ := strconv.Atoi(f[0])
		if level != 5 {
			continue
		}
		conf, _ := strconv.ParseFloat(f[10], 64)
		txt := strings.TrimSpace(f[11])
		if txt == "" || conf < 0 {
			continue
		}
		w := Word{Text: txt, Conf: conf}
		w.Block, _ = strconv.Atoi(f[2])
		w.Par, _ = strconv.Atoi(f[3])
		w.Line, _ = strconv.Atoi(f[4])
		w.X, _ = strconv.Atoi(f[6])
		w.Y, _ = strconv.Atoi(f[7])
		w.W, _ = strconv.Atoi(f[8])
		w.H, _ = strconv.Atoi(f[9])
		key := w.Block*100000 + w.Par*1000 + w.Line
		if key != lastLine && text.Len() > 0 {
			text.WriteByte('\n')
		} else if text.Len() > 0 {
			text.WriteByte(' ')
		}
		lastLine = key
		text.WriteString(txt)
		res.Words = append(res.Words, w)
	}
	res.Text = text.String()
	return res, sc.Err()
}

// SKU-like token: 4–24 chars of A-Z 0-9 . / - with at least two digits and (a letter or a dash).
var (
	reSKU       = regexp.MustCompile(`^[A-Z0-9][A-Z0-9./-]{2,22}[A-Z0-9]$`)
	reFraction  = regexp.MustCompile(`\d+/\d+`)
	reDimension = regexp.MustCompile(`^\d+(\.\d+)?(MM|CM|IN|GPM|PSI|LB|LBS|KG|W|V|HZ)$`)
)

// LooksLikeSKU is a generic heuristic; brand-specific patterns can refine it later.
func LooksLikeSKU(t string) bool {
	t = strings.Trim(strings.ToUpper(t), ",;:()[]")
	if !reSKU.MatchString(t) {
		return false
	}
	digits, letters, dashes := 0, 0, 0
	for _, r := range t {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'A' && r <= 'Z':
			letters++
		case r == '-':
			dashes++
		}
	}
	if digits < 2 {
		return false
	}
	if strings.ContainsAny(t, "$\"°%") || reFraction.MatchString(t) || reDimension.MatchString(t) {
		return false // prices, inches, angles, 7-15/16 style dimensions, 57MM
	}
	// pure article numbers (Gessi 37231, Kohler 12345) are 5–8 digits; shorter digit runs are
	// finish codes, page numbers or years.
	if letters == 0 && dashes == 0 {
		return len(t) >= 5 && len(t) <= 8 && !strings.Contains(t, ".")
	}
	if letters == 0 && dashes == 1 && len(t) <= 5 {
		return false
	}
	return true
}

func Normalize(t string) string {
	return strings.Trim(strings.ToUpper(t), ",;:()[]")
}
