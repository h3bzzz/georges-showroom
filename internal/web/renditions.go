package web

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"

	"georges/internal/assets"
	"georges/internal/store"
)

// renditions makes the two kinds of derived image the server may still produce at request
// time: on-demand sizes of mutable files (covers, logos, catalog pages) and, in development
// only, the ladder of an asset that has not been built yet.
type renditions struct {
	dataDir string
	st      *store.Store
	g       singleflight.Group
}

func newRenditions(dataDir string, st *store.Store) *renditions {
	return &renditions{dataDir: dataDir, st: st}
}

// file returns the path of rel resized to width w (WebP), creating it when missing or stale.
func (r *renditions) file(rel string, w int) (string, error) {
	src := filepath.Join(r.dataDir, filepath.FromSlash(rel))
	out := filepath.Join(r.dataDir, "cache", "img", fmt.Sprint(w), filepath.FromSlash(strings.TrimSuffix(rel, filepath.Ext(rel))+".webp"))
	sst, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if ost, err := os.Stat(out); err == nil && !ost.ModTime().Before(sst.ModTime()) {
		return out, nil
	}
	_, err, _ = r.g.Do(out, func() (any, error) {
		img, _, err := assets.Decode(src)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		tw := min(w, b.Dx())
		th := max(1, b.Dy()*tw/max(1, b.Dx()))
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, assets.WriteWebP(out, dst)
	})
	return out, err
}

// asset builds the whole ladder for one asset identified by its short hash (dev only).
func (r *renditions) asset(ctx context.Context, short string) error {
	_, err, _ := r.g.Do("asset:"+short, func() (any, error) {
		var id int64
		var hash, src string
		err := r.st.Pool.QueryRow(ctx, `SELECT id, hash, src_path FROM assets WHERE hash LIKE $1 || '%' LIMIT 1`, short).Scan(&id, &hash, &src)
		if err != nil {
			return nil, err
		}
		abs, err := assets.Abs(r.dataDir, src)
		if err != nil {
			return nil, err
		}
		img, format, err := assets.Decode(abs)
		if err != nil {
			return nil, err
		}
		m := assets.Analyze(img)
		if _, err := assets.Build(r.dataDir, hash, img, m); err != nil {
			return nil, err
		}
		_, err = r.st.Pool.Exec(ctx, `UPDATE assets SET format=$2, width=$3, height=$4, bg_white=$5, color=$6, fill=$7, sharpness=$8, lqip=$9, built=$10 WHERE id=$1`,
			id, format, m.Width, m.Height, m.BgWhite, m.Color, m.Fill, m.Sharpness, m.LQIP, assets.LadderVersion)
		return nil, err
	})
	return err
}
