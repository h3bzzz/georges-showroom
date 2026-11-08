package specs

import "testing"

func TestExtractNewport(t *testing.T) {
	text := `Features
Solid brass construction
Euro-motion ceramic disc 35 mm valve cartridge
Magnetic docking system secures spray engine to spout
10.70'' (27.2 cm) spout height at outlet
8.53'' (21.7 cm) spout reach (c-c)
ADA compliant (Handles only)
1.8 GPM (6.8 LPM) Max
Codes / Standards Compliance
Meets or exceeds the following at date of manufacture:
ASME A112.18.1/CSA B125.1
IAPMO/cUPC
NSF/ANSI/CAN 61, SECTION 9
ADA & ICC/ANSI A117.1 (Handle Only)`
	m := Extract(text)
	want := map[string]string{"Flow Rate": "1.8", "Spout Height": "10.70", "Spout Reach": "8.53", "Material": "Solid brass", "Cartridge": "35 mm ceramic disc", "ADA Compliant": "Yes (handles)"}
	for k, v := range want {
		if got := m.Facts[k].Value; got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	if c := m.Facts["Certifications"].Value; c == "" || !contains(c, "cUPC") || !contains(c, "NSF/ANSI 61") {
		t.Errorf("certs: %q", c)
	}
	if len(m.Bullets) < 3 || m.Bullets[0] != "Solid brass construction" {
		t.Errorf("bullets: %v", m.Bullets)
	}
}

func TestExtractBlancoAndChicago(t *testing.T) {
	m := Extract(`PRECIS 21
Sink Specifications SILGRANIT
Design and Planning Tips
• Min cabinet size: 24 in
• Bowl depth: 7,5 in ( 191.0 mm )
Undermount installation only
Minimum cabinet size: 24''
Cut-out Length: 19.94''
Cut-out Width: 16''
Sink Bowl Length (left-to-right): 19.75''
SinkBowl Width (front-to-back): 15.75''
Sink Bowl Depth: 7.5''
Codes/Standards: CSA B45.8-18/IAPMO Z403-2018
BLANCO offers a limited lifetime warranty on all sinks`)
	want := map[string]string{"Mounting": "Undermount", "Minimum Cabinet": "24", "Cut-out Length": "19.94", "Cut-out Width": "16", "Bowl Depth": "7.5", "Bowl Length": "19.75", "Bowl Width": "15.75", "Material": "SILGRANIT granite composite", "Warranty": "Limited lifetime"}
	for k, v := range want {
		if got := m.Facts[k].Value; got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	c := Extract(`Laboratory Fittings
LGN3-A21K-10
Product Type
Deck-mounted laboratory turret with needle valve
Features & Specifications Job Name ____________
Rated Operating Pressure: 125 PSI max. (for Acetylene service)
Codes & Standards
ADA ANSI/ICC A117.1`)
	if c.Type != "Deck-mounted laboratory turret with needle valve" {
		t.Errorf("type: %q", c.Type)
	}
	if c.Facts["Max Pressure"].Value != "125" || c.Facts["Mounting"].Value != "Deck mount" {
		t.Errorf("facts: %+v", c.Facts)
	}
	if len(c.Bullets) != 0 {
		t.Errorf("no junk bullets wanted, got %v", c.Bullets)
	}
	if Extract("").Facts == nil {
		t.Error("empty")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
