package upscale

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Model is an RRDBNet x4 network in ncnn format that realesrgan-ncnn-vulkan can load.
type Model struct {
	Name    string // file stem under <tool dir>/models
	Param   string // URL of the .param (ncnn graph)
	Bin     string // URL of the .bin (weights)
	License string
	Note    string
}

// Models the tool knows how to install. realesrgan-x4plus ships with the binary.
var Models = map[string]Model{
	"4xNomos8kSC_fp16": {
		Name:    "4xNomos8kSC_fp16",
		Param:   "https://raw.githubusercontent.com/Phhofm/models/main/4xNomos8kSC/4xNomos8kSC_fp16.param",
		Bin:     "https://raw.githubusercontent.com/Phhofm/models/main/4xNomos8kSC/4xNomos8kSC_fp16.bin",
		License: "CC BY 4.0",
		Note:    "trained on JPEG-compressed, blurred real photos: smoother metal, less edge halo than x4plus; same speed",
	},
}

// EnsureModel makes <bin dir>/models/<name>.{param,bin} exist, downloading a known model on
// first use. realesrgan-ncnn-vulkan hard-codes the input blob name "data" while converted
// models name it "input", so the param graph is patched on the way in.
func EnsureModel(ctx context.Context, bin, name string) error {
	dir := filepath.Join(filepath.Dir(bin), "models")
	param, weights := filepath.Join(dir, name+".param"), filepath.Join(dir, name+".bin")
	if exists(param) && exists(weights) {
		return nil
	}
	m, ok := Models[name]
	if !ok {
		return fmt.Errorf("model %q is not installed and not in the known list %v", name, modelNames())
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Printf("downloading model %s (%s)…\n", name, m.License)
	p, err := fetch(ctx, m.Param)
	if err != nil {
		return err
	}
	p = patchInputBlob(p)
	if err := os.WriteFile(param, p, 0o644); err != nil {
		return err
	}
	w, err := fetch(ctx, m.Bin)
	if err != nil {
		return err
	}
	if len(w) < 1<<20 {
		return fmt.Errorf("model %s: weights are %d bytes (an LFS pointer, not a model?)", name, len(w))
	}
	return os.WriteFile(weights, w, 0o644)
}

// patchInputBlob renames the graph's input blob to "data". The blob appears twice: as the
// output of the Input layer and as the first consumer's input; both are the whole token.
func patchInputBlob(param []byte) []byte {
	lines := bytes.Split(param, []byte("\n"))
	var blob []byte
	for i, ln := range lines {
		f := bytes.Fields(ln)
		if len(f) == 0 {
			continue
		}
		if blob == nil {
			if string(f[0]) == "Input" && len(f) >= 5 {
				blob = f[len(f)-1]
				if string(blob) == "data" {
					return param
				}
				f[len(f)-1] = []byte("data")
				lines[i] = bytes.Join(f, []byte(" "))
			}
			continue
		}
		for j := range f {
			if bytes.Equal(f[j], blob) {
				f[j] = []byte("data")
			}
		}
		lines[i] = bytes.Join(f, []byte(" "))
	}
	return bytes.Join(lines, []byte("\n"))
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(res.Body)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func modelNames() []string {
	out := make([]string, 0, len(Models)+1)
	out = append(out, DefaultModel)
	for k := range Models {
		out = append(out, k)
	}
	return out
}
