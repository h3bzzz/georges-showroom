package upscale

import "testing"

func TestPatchInputBlob(t *testing.T) {
	in := "7767517\n3 3\nInput            input                    0 1 input\nConvolution      Conv_0                   1 1 input 703 0=64\nConvolution      Conv_1 1 1 703 output 0=3\n"
	got := string(patchInputBlob([]byte(in)))
	want := "7767517\n3 3\nInput input 0 1 data\nConvolution Conv_0 1 1 data 703 0=64\nConvolution Conv_1 1 1 703 output 0=3\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	already := "7767517\n2 2\nInput input.1 0 1 data\nConvolution c 1 1 data out\n"
	if string(patchInputBlob([]byte(already))) != already {
		t.Fatal("already-patched graph must be untouched")
	}
}
