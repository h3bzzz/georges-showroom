package taxonomy

import (
	"path"
	"strings"
)

// PicturedFinish reads the finish a vendor master really shows from its file name
// (3001-1T-SN.jpg, T67430-RB-B1.png, l018-102-c1?wid=1600): the last token after the model
// that is one of the brand's finish codes. It is "" when the name carries no code, so
// nothing can be claimed about the picture.
func PicturedFinish(codes map[string]string, u string) string {
	if u == "" || len(codes) == 0 {
		return ""
	}
	name := path.Base(u)
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimSuffix(name, path.Ext(name))
	toks := strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' })
	for i := len(toks) - 1; i >= 1; i-- {
		if f, ok := codes[toks[i]]; ok {
			return f
		}
	}
	return ""
}

// SameFinish compares two finish names loosely (case and spacing).
func SameFinish(a, b string) bool {
	norm := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
	return norm(a) == norm(b)
}

// FinishTone is the tone group a finish name reads as (silver, gold, bronze, black, white…).
func FinishTone(name string) string {
	e := ClassifyFinish(name)
	if e.Head != "" {
		return ToneGroup(e.Head)
	}
	if len(e.Tones) > 0 {
		return ToneGroup(e.Tones[0])
	}
	return "other"
}
