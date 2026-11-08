// Package vision embeds catalog crops with CLIP (ONNX Runtime through hugot) and classifies
// them by medium (photo, close-up, drawing, spec, text) and subject (faucet, handle, sink, …).
// Pixel statistics cannot tell a grey CAD render from a chrome product photo or a handle
// close-up from a whole faucet; a vision-language model can, and its embeddings double as
// the index for "find the product that looks like this picture".
package vision

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ORTVersion is the ONNX Runtime release downloaded on first use. hugot v0.8 asks for ORT API
// 27, which 1.31 provides; older releases work but log a warning per missing API level.
const ORTVersion = "1.31.0"

// Model is a CLIP checkpoint exported to ONNX by the transformers.js project (one file per
// tower, so the vision tower runs alone per image and the text tower once per prompt set).
type Model struct {
	Repo    string // Hugging Face repo
	License string
	Note    string
}

// Models the tool knows how to install. ViT-B/16 is the default: measurably better than
// B/32 on small catalog crops at ~4× the CPU cost (≈170 ms per crop on four cores).
var Models = map[string]Model{
	"clip-vit-base-patch16": {Repo: "Xenova/clip-vit-base-patch16", License: "MIT (OpenAI CLIP weights)", Note: "default; 512-d; 224 px"},
	"clip-vit-base-patch32": {Repo: "Xenova/clip-vit-base-patch32", License: "MIT (OpenAI CLIP weights)", Note: "faster, coarser patches"},
}

// DefaultModel is used when no --model is given.
const DefaultModel = "clip-vit-base-patch16"

var modelFiles = []string{
	"config.json", "preprocessor_config.json", "tokenizer.json", "tokenizer_config.json",
	"special_tokens_map.json", "vocab.json", "merges.txt",
	"onnx/vision_model.onnx", "onnx/text_model.onnx",
}

// RuntimeDir is where the ONNX Runtime shared library lives: <dataDir>/tools/onnxruntime/<ver>.
func RuntimeDir(dataDir string) string {
	return filepath.Join(dataDir, "tools", "onnxruntime", ORTVersion)
}

// ModelDir is where a model's files live: <dataDir>/tools/clip/<name>.
func ModelDir(dataDir, name string) string {
	return filepath.Join(dataDir, "tools", "clip", name)
}

// EnsureRuntime downloads the ONNX Runtime release tarball once and keeps only the shared
// libraries, under RuntimeDir. Returns the directory hugot's WithOnnxLibraryPath wants.
func EnsureRuntime(ctx context.Context, dataDir string) (string, error) {
	dir, err := filepath.Abs(RuntimeDir(dataDir))
	if err != nil {
		return "", err
	}
	if exists(filepath.Join(dir, "libonnxruntime.so")) {
		return dir, nil
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "", fmt.Errorf("onnxruntime auto-install only knows linux/amd64; put libonnxruntime.so in %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://github.com/microsoft/onnxruntime/releases/download/v%s/onnxruntime-linux-x64-%s.tgz", ORTVersion, ORTVersion)
	fmt.Printf("downloading ONNX Runtime %s…\n", ORTVersion)
	body, err := fetch(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	gz, err := gzip.NewReader(body)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	got := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		base := filepath.Base(h.Name)
		if h.Typeflag != tar.TypeReg || !strings.Contains(h.Name, "/lib/") || !strings.HasPrefix(base, "libonnxruntime") {
			continue
		}
		// libonnxruntime.so.1.31.0 → libonnxruntime.so (hugot opens the unversioned name)
		name := base
		if strings.HasPrefix(base, "libonnxruntime.so.") {
			name = "libonnxruntime.so"
		}
		if err := writeAtomic(filepath.Join(dir, name), tr, 0o755); err != nil {
			return "", err
		}
		got++
	}
	if !exists(filepath.Join(dir, "libonnxruntime.so")) {
		return "", fmt.Errorf("onnxruntime tarball had no libonnxruntime.so (%d libs extracted)", got)
	}
	return dir, nil
}

// EnsureModel downloads a known model's ONNX towers and tokenizer files once.
func EnsureModel(ctx context.Context, dataDir, name string) (string, error) {
	m, ok := Models[name]
	if !ok {
		return "", fmt.Errorf("model %q is not in the known list %v", name, modelNames())
	}
	dir := ModelDir(dataDir, name)
	missing := false
	for _, f := range modelFiles {
		if !exists(filepath.Join(dir, filepath.Base(f))) {
			missing = true
			break
		}
	}
	if !missing {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fmt.Printf("downloading %s (%s)…\n", m.Repo, m.License)
	for _, f := range modelFiles {
		dst := filepath.Join(dir, filepath.Base(f))
		if exists(dst) {
			continue
		}
		body, err := fetch(ctx, "https://huggingface.co/"+m.Repo+"/resolve/main/"+f)
		if err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		err = writeAtomic(dst, body, 0o644)
		body.Close()
		if err != nil {
			return "", err
		}
		if st, _ := os.Stat(dst); strings.HasSuffix(f, ".onnx") && st != nil && st.Size() < 10<<20 {
			os.Remove(dst)
			return "", fmt.Errorf("%s: %d bytes (an LFS pointer, not a model?)", f, st.Size())
		}
	}
	return dir, nil
}

func fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "georges-vision/1.0")
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

func writeAtomic(dst string, r io.Reader, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".part-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func modelNames() []string {
	var n []string
	for k := range Models {
		n = append(n, k)
	}
	return n
}
