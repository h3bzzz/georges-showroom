// Package upscale runs Real-ESRGAN (ncnn-vulkan build, no Python/CUDA) over small catalog
// crops. Flipbook pages are only ~1700x2200, so a product photo on a price-book page is often
// under 250 px wide; 4x super-resolution is what makes them presentable on a retina grid.
package upscale

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// prebuilt Linux binary + models from the upstream release (≈47 MB, extracted once)
	ReleaseURL   = "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-ubuntu.zip"
	DefaultModel = "realesrgan-x4plus" // photos; realesr-animevideov3-x4 is ~25x faster but waxy
	binName      = "realesrgan-ncnn-vulkan"
)

// Ensure returns the path to the binary under <dataDir>/tools/realesrgan, downloading and
// extracting the release on first use.
func Ensure(ctx context.Context, dataDir string) (string, error) {
	dir, err := filepath.Abs(filepath.Join(dataDir, "tools", "realesrgan"))
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, binName)
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download realesrgan: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download realesrgan: HTTP %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(dir, "release-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", err
	}
	tmp.Close()
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || f.FileInfo().IsDir() {
			continue
		}
		dst := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()|0o644)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return "", err
		}
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return "", err
	}
	return bin, nil
}

// Options control one batch run.
type Options struct {
	Model string // model name under <tool dir>/models
	Scale int    // 4 for the x4 models
	GPU   int    // vulkan device index; -1 = CPU
}

// RunDir upscales every image in inDir into outDir as PNG (same base names). ncnn processes a
// whole directory per process, which amortises the ~1 s Vulkan/shader startup.
func RunDir(ctx context.Context, bin, inDir, outDir string, o Options) error {
	if o.Model == "" {
		o.Model = DefaultModel
	}
	if o.Scale == 0 {
		o.Scale = 4
	}
	// the process runs in the tool dir (models/ is looked up relative to cwd), so paths must be absolute
	inDir, _ = filepath.Abs(inDir)
	outDir, _ = filepath.Abs(outDir)
	args := []string{"-i", inDir, "-o", outDir, "-n", o.Model, "-s", strconv.Itoa(o.Scale), "-f", "png", "-g", strconv.Itoa(o.GPU)}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = filepath.Dir(bin) // models/ is resolved relative to the working directory
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if i := strings.LastIndex(msg, "\n"); i > 0 && len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("%s: %w: %s", binName, err, strings.TrimSpace(msg))
	}
	return nil
}

// SaveJPEG re-encodes an upscaled PNG as a JPEG (quality q) at dst and returns its width.
func SaveJPEG(src, dst string, q int) (int, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: q}); err != nil {
		return 0, err
	}
	return img.Bounds().Dx(), nil
}
