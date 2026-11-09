package media

import "testing"

func TestNearest(t *testing.T) {
	// a page 1700×2200: two photos side by side, model numbers printed under each
	crops := []Box{{100, 200, 500, 500}, {900, 200, 500, 500}}
	diag := 2780.0
	if i, w := Nearest(Box{150, 720, 200, 30}, crops, diag); i != 0 || w < 0.8 {
		t.Errorf("caption under the left photo: %d %.2f", i, w)
	}
	if i, _ := Nearest(Box{1000, 720, 200, 30}, crops, diag); i != 1 {
		t.Errorf("caption under the right photo: %d", i)
	}
	// a number in a price table at the bottom of the page captions nothing
	if i, _ := Nearest(Box{800, 2000, 200, 30}, crops, diag); i != -1 {
		t.Errorf("far table row should not link: %d", i)
	}
	// a wide photo with its caption far from the centre but right under the edge
	if i, w := Nearest(Box{1300, 910, 150, 30}, []Box{{100, 300, 1400, 600}}, diag); i != 0 || w < 0.85 {
		t.Errorf("edge caption: %d %.2f", i, w)
	}
	if i, _ := Nearest(Box{0, 0, 10, 10}, nil, diag); i != -1 {
		t.Error("no crops")
	}
}
