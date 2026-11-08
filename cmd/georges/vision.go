package main

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"georges/internal/assets"
	"georges/internal/chat"
	"georges/internal/vision"
)

// openMatcher loads the look-alike index (and the encoder when built in) for the assistant.
func openMatcher(ctx context.Context, pool *pgxpool.Pool) (chat.Matcher, error) {
	m, err := vision.NewMatcher(ctx, pool, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return matcherAdapter{m}, nil
}

type matcherAdapter struct{ m *vision.Matcher }

func (a matcherAdapter) MatchImage(ctx context.Context, img []byte, n int) ([]chat.Match, error) {
	hits, err := a.m.MatchBytes(ctx, img, n)
	return toMatches(hits), err
}

func (a matcherAdapter) Similar(_ context.Context, fid int64, n int) ([]chat.Match, error) {
	hits, err := a.m.Similar(fid, n)
	return toMatches(hits), err
}

func toMatches(hits []vision.Hit) []chat.Match {
	out := make([]chat.Match, len(hits))
	for i, h := range hits {
		out[i] = chat.Match{FamilyID: h.FamilyID, Score: h.Score}
	}
	return out
}

func visionCmd() *cobra.Command {
	c := &cobra.Command{Use: "vision", Short: "CLIP embeddings: the look-alike index behind the assistant"}
	var limit int
	embed := &cobra.Command{
		Use:   "embed",
		Short: "Embed every shown asset that has no vector yet (vendor masters, new crops)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			enc, err := vision.Open(cmd.Context(), vision.Options{DataDir: cfg.DataDir, Threads: runtime.NumCPU()})
			if err != nil {
				return err
			}
			defer enc.Close()
			return embedMissing(cmd.Context(), pool, enc, limit)
		},
	}
	embed.Flags().IntVar(&limit, "limit", 0, "stop after N assets (0 = all)")
	match := &cobra.Command{
		Use:   "match IMAGE",
		Short: "Print the products that look like a picture",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			m, err := vision.NewMatcher(cmd.Context(), pool, cfg.DataDir)
			if err != nil {
				return err
			}
			defer m.Close()
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			t := time.Now()
			hits, err := m.MatchBytes(cmd.Context(), raw, 10)
			if err != nil {
				return err
			}
			return printHits(cmd.Context(), pool, hits, time.Since(t))
		},
	}
	similar := &cobra.Command{
		Use:   "similar FAMILY_ID",
		Short: "Print the products whose card looks like a family's card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			ix, err := vision.Load(cmd.Context(), pool)
			if err != nil {
				return err
			}
			var fid int64
			fmt.Sscan(args[0], &fid)
			t := time.Now()
			hits, err := ix.Similar(fid, 10)
			if err != nil {
				return err
			}
			return printHits(cmd.Context(), pool, hits, time.Since(t))
		},
	}
	c.AddCommand(embed, match, similar)
	return c
}

func printHits(ctx context.Context, pool *pgxpool.Pool, hits []vision.Hit, took time.Duration) error {
	fmt.Printf("%d hits in %s\n", len(hits), took.Round(time.Millisecond))
	for _, h := range hits {
		var brand, title, cat string
		_ = pool.QueryRow(ctx, `SELECT brand, title, COALESCE(category,'') FROM search_docs WHERE family_id=$1`, h.FamilyID).Scan(&brand, &title, &cat)
		fmt.Printf("  %.3f  /p/%-7d %s · %s · %s\n", h.Score, h.FamilyID, brand, title, cat)
	}
	return nil
}

// embedMissing encodes shown assets without a vector, 16 at a time, and stores them.
func embedMissing(ctx context.Context, pool *pgxpool.Pool, enc *vision.Encoder, limit int) error {
	q := `
		SELECT DISTINCT a.id, a.src_path FROM family_media fm JOIN assets a ON a.id=fm.asset_id
		WHERE a.embedding IS NULL AND NOT a.rejected AND a.built <> -1 ORDER BY a.id`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return err
	}
	type job struct {
		id   int64
		path string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.path); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	slog.Info("vision embed", "assets", len(jobs), "model", enc.Model)
	t := time.Now()
	done := 0
	for i := 0; i < len(jobs); i += vision.BatchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		j := min(len(jobs), i+vision.BatchSize)
		var imgs []image.Image
		var ids []int64
		for _, jb := range jobs[i:j] {
			abs, err := assets.Abs(cfg.DataDir, jb.path)
			if err != nil {
				continue
			}
			img, _, err := assets.Decode(abs)
			if err != nil {
				slog.Warn("vision embed: decode", "path", jb.path, "err", err)
				continue
			}
			imgs = append(imgs, img)
			ids = append(ids, jb.id)
		}
		if len(imgs) == 0 {
			continue
		}
		vecs, err := enc.Images(ctx, imgs)
		if err != nil {
			return err
		}
		for k, v := range vecs {
			if _, err := pool.Exec(ctx, `UPDATE assets SET embedding=$2, emb_model=$3 WHERE id=$1`, ids[k], vision.Encode(v), enc.Model); err != nil {
				return err
			}
		}
		done += len(vecs)
		if done%320 < vision.BatchSize {
			slog.Info("vision embed", "done", done, "of", len(jobs), "rate/s", fmt.Sprintf("%.1f", float64(done)/time.Since(t).Seconds()))
		}
	}
	fmt.Printf("%d assets embedded in %s\n", done, time.Since(t).Round(time.Second))
	return nil
}

var _ = filepath.Join
