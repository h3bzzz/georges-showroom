package vision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"sync"
	"time"

	_ "github.com/gen2brain/webp" // registers the webp decoder

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUnavailable is returned when the binary was built without `-tags ORT` (and cgo): the
// stored embeddings still serve look-alike search, but new pictures cannot be encoded.
var ErrUnavailable = errors.New("vision: encoder not built in (build with -tags ORT and CGO_ENABLED=1)")

// Matcher answers "what do we carry that looks like this?": a photograph is encoded with the
// CLIP vision tower and compared with every picture the site shows. The encoder is optional;
// without it only Similar (family to family) works, from stored vectors.
type Matcher struct {
	Index *Index
	enc   *Encoder
	mu    sync.Mutex // the ORT session is used one image at a time
}

// NewMatcher loads the index and, when the encoder is built in, the CLIP towers.
func NewMatcher(ctx context.Context, pool *pgxpool.Pool, dataDir string) (*Matcher, error) {
	t := time.Now()
	ix, err := Load(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("vision index: %w", err)
	}
	m := &Matcher{Index: ix}
	enc, err := Open(ctx, Options{DataDir: dataDir, Threads: 2})
	if err != nil {
		slog.Info("vision: look-alikes from stored vectors only", "vectors", ix.Len(), "reason", err, "took", time.Since(t).Round(time.Millisecond))
		return m, nil
	}
	m.enc = enc
	slog.Info("vision: matcher ready", "vectors", ix.Len(), "dim", ix.Dim(), "model", enc.Model, "took", time.Since(t).Round(time.Millisecond))
	return m, nil
}

// CanEncode reports whether photographs can be matched.
func (m *Matcher) CanEncode() bool { return m != nil && m.enc != nil }

// Close releases the encoder.
func (m *Matcher) Close() error {
	if m == nil || m.enc == nil {
		return nil
	}
	return m.enc.Close()
}

// Embed encodes one decoded image.
func (m *Matcher) Embed(ctx context.Context, img image.Image) ([]float32, error) {
	if m.enc == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vs, err := m.enc.Images(ctx, []image.Image{img})
	if err != nil {
		return nil, err
	}
	if len(vs) != 1 {
		return nil, fmt.Errorf("vision: %d vectors for one image", len(vs))
	}
	return vs[0], nil
}

// MatchBytes decodes and matches an encoded picture.
func (m *Matcher) MatchBytes(ctx context.Context, raw []byte, n int) ([]Hit, error) {
	if m.enc == nil {
		return nil, ErrUnavailable
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("vision: decode: %w", err)
	}
	v, err := m.Embed(ctx, img)
	if err != nil {
		return nil, err
	}
	return m.Index.Search(v, n, 0), nil
}

// Similar finds families whose card looks like a family's card.
func (m *Matcher) Similar(fid int64, n int) ([]Hit, error) { return m.Index.Similar(fid, n) }
