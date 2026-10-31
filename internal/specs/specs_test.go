package specs

import (
	"path/filepath"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"http://georges.Requested-Info.com/brands/ICE/documents/specSheets/ICEC6360S.PDF": "https://documents.cx1accel.com/brands/ICE/documents/specSheets/ICEC6360S.PDF",
		"https://georges.requested-info.com/brands/NAT/documents/installSheets/x.pdf":     "https://documents.cx1accel.com/brands/NAT/documents/installSheets/x.pdf",
		"https://documents.cx1accel.com/brands/CAL/documents/specSheets/K30-123-XX_sp.pdf": "https://documents.cx1accel.com/brands/CAL/documents/specSheets/K30-123-XX_sp.pdf",
		"https://example.com/other.pdf": "https://example.com/other.pdf",
	}
	for in, want := range cases {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
	got := LocalPath("docs", "http://georges.Requested-Info.com/brands/ICE/documents/specSheets/ICEC6360S.PDF?x=1")
	if got != filepath.Join("docs", "ICE", "specSheets", "ICEC6360S.PDF") {
		t.Errorf("LocalPath = %q", got)
	}
}
