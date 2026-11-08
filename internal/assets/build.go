package assets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Builder measures assets and writes their rendition ladder. It is resumable: an asset is
// picked while built < LadderVersion and marked when its files are on disk, so a killed run
// continues where it stopped. Workers equal the CPU count; decoding and WebP encoding are
// the whole cost.
type Builder struct {
	Pool    *pgxpool.Pool
	DataDir string
	Workers int
	Limit   int    // 0 = all pending
	Source  string // restrict to one source ("vendor", "catalog", …)
	// Shown restricts the pass to assets referenced by family_media (the ones pages use),
	// so the visible site is right before the long tail is processed.
	Shown bool
}

type pending struct {
	ID            int64
	SrcPath       string
	Kind          *string
	KindScore     *float32
	Width, Height *int
}

// Run processes pending assets and returns how many it built.
func (b *Builder) Run(ctx context.Context) (int, error) {
	if b.Workers <= 0 {
		b.Workers = runtime.NumCPU()
	}
	where := `a.built < $1 AND NOT a.rejected AND a.dup_of IS NULL`
	args := []any{LadderVersion}
	if b.Source != "" {
		args = append(args, b.Source)
		where += fmt.Sprintf(` AND a.source = $%d`, len(args))
	}
	if b.Shown {
		where += ` AND EXISTS (SELECT 1 FROM family_media fm WHERE fm.asset_id = a.id)`
	}
	limit := ""
	if b.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", b.Limit)
	}
	rows, err := b.Pool.Query(ctx, `SELECT a.id, a.src_path, a.kind, a.kind_score, a.width, a.height FROM assets a WHERE `+where+` ORDER BY a.id`+limit, args...)
	if err != nil {
		return 0, err
	}
	todo, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
	if err != nil {
		return 0, err
	}
	slog.Info("assets build", "pending", len(todo), "workers", b.Workers)
	jobs := make(chan pending)
	var done, failed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < b.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if err := b.one(ctx, p); err != nil {
					failed.Add(1)
					slog.Warn("asset", "id", p.ID, "path", p.SrcPath, "err", err)
					// a broken file is marked so it is not retried forever; -1 keeps it off pages
					b.Pool.Exec(ctx, `UPDATE assets SET built = -1 WHERE id = $1`, p.ID)
					continue
				}
				if n := done.Add(1); n%500 == 0 {
					rate := float64(n) / time.Since(start).Seconds()
					slog.Info("assets build", "done", n, "failed", failed.Load(), "per_sec", fmt.Sprintf("%.1f", rate),
						"eta", (time.Duration(float64(len(todo)-int(n))/rate) * time.Second).Round(time.Second))
				}
			}
		}()
	}
	for _, p := range todo {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return int(done.Load()), ctx.Err()
		case jobs <- p:
		}
	}
	close(jobs)
	wg.Wait()
	slog.Info("assets build finished", "built", done.Load(), "failed", failed.Load(), "took", time.Since(start).Round(time.Second))
	return int(done.Load()), nil
}

func (b *Builder) one(ctx context.Context, p pending) error {
	abs, err := Abs(b.DataDir, p.SrcPath)
	if err != nil {
		return err
	}
	img, format, err := Decode(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("missing source")
		}
		return err
	}
	m := Analyze(img)
	phash, pdq, err := Perceptual(img)
	if err != nil {
		return err
	}
	var hash string
	if err := b.Pool.QueryRow(ctx, `SELECT hash FROM assets WHERE id = $1`, p.ID).Scan(&hash); err != nil {
		return err
	}
	if _, err := Build(b.DataDir, hash, img, m); err != nil {
		return err
	}
	kind := ""
	var ks float32
	if p.Kind != nil {
		kind = *p.Kind
	}
	if p.KindScore != nil {
		ks = *p.KindScore
	}
	q := Quality(m, kind, ks)
	_, err = b.Pool.Exec(ctx, `
		UPDATE assets SET format=$2, width=$3, height=$4, bg_white=$5, color=$6, fill=$7, sharpness=$8, lqip=$9,
		  phash=$10, pdq=$11, quality=$12, built=$13, cut=$20,
		  origin = origin || jsonb_build_object('src_w', $14::int, 'src_h', $15::int, 'trim', jsonb_build_array($16::int,$17::int,$18::int,$19::int))
		WHERE id=$1`,
		p.ID, format, m.Width, m.Height, m.BgWhite, m.Color, m.Fill, m.Sharpness, m.LQIP,
		phash, pdq, q, LadderVersion,
		img.Bounds().Dx(), img.Bounds().Dy(), m.Trim.Min.X, m.Trim.Min.Y, m.Trim.Max.X, m.Trim.Max.Y, m.Cut)
	return err
}

// Measure recomputes the measures (no renditions) for assets that lack a value the rules
// now need, such as cut. It decodes each file once; ~60 pictures/s per core.
func (b *Builder) Measure(ctx context.Context) (int, error) {
	if b.Workers <= 0 {
		b.Workers = runtime.NumCPU()
	}
	limit := ""
	if b.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", b.Limit)
	}
	rows, err := b.Pool.Query(ctx, `SELECT a.id, a.src_path, a.kind, a.kind_score, a.width, a.height FROM assets a WHERE a.cut IS NULL AND a.built >= 0 AND NOT a.rejected ORDER BY (SELECT count(*) FROM family_media fm WHERE fm.asset_id=a.id) DESC, a.id`+limit)
	if err != nil {
		return 0, err
	}
	todo, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
	if err != nil {
		return 0, err
	}
	slog.Info("assets measure", "pending", len(todo), "workers", b.Workers)
	jobs := make(chan pending)
	var done atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < b.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				abs, err := Abs(b.DataDir, p.SrcPath)
				if err != nil {
					continue
				}
				img, _, err := Decode(abs)
				if err != nil {
					b.Pool.Exec(ctx, `UPDATE assets SET cut = 0 WHERE id = $1`, p.ID)
					continue
				}
				m := Analyze(img)
				kind, ks := "", float32(0)
				if p.Kind != nil {
					kind = *p.Kind
				}
				if p.KindScore != nil {
					ks = *p.KindScore
				}
				b.Pool.Exec(ctx, `UPDATE assets SET cut=$2, bg_white=$3, fill=$4, sharpness=$5, lqip=$6, quality=$7 WHERE id=$1`,
					p.ID, m.Cut, m.BgWhite, m.Fill, m.Sharpness, m.LQIP, Quality(m, kind, ks))
				if n := done.Add(1); n%1000 == 0 {
					slog.Info("assets measure", "done", n, "per_sec", fmt.Sprintf("%.1f", float64(n)/time.Since(start).Seconds()))
				}
			}
		}()
	}
	for _, p := range todo {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return int(done.Load()), ctx.Err()
		case jobs <- p:
		}
	}
	close(jobs)
	wg.Wait()
	return int(done.Load()), nil
}
