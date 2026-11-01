package main

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"georges_/internal/store"
	"georges_/internal/vision"
)

// catalogsVisionCmd embeds every crop with CLIP and classifies it by medium (photo, close-up,
// drawing, dimensioned spec, text) and subject (faucet, handle, sink, …). The verdict feeds
// crop_kind() (what may be a card photo at all) and subject_fits() (which crop may stand for
// a family of a given category), then family photos are re-picked.
func catalogsVisionCmd() *cobra.Command {
	var brand, model, labels, rank string
	var limit, threads, top int
	var all, dry, eval, reclassify, spec, tone, scene, fit bool
	var mix float32
	var show []string
	cmd := &cobra.Command{
		Use:   "vision",
		Short: "Classify crops with CLIP (photo | detail | drawing | spec | text; subject) and re-pick family photos",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			ctx := cmd.Context()
			enc, err := vision.Open(ctx, vision.Options{DataDir: cfg.DataDir, Model: model, Threads: threads})
			if err != nil {
				return err
			}
			defer enc.Close()
			if tone {
				return runTone(ctx, cfg.DatabaseURL, enc, brand, mix, dry)
			}
			if scene {
				return runScene(ctx, cfg.DatabaseURL, enc, brand, dry)
			}
			if fit {
				return runFit(ctx, cfg.DatabaseURL, enc, brand, dry)
			}
			medium, err := enc.NewHead(ctx, vision.MediumClasses)
			if err != nil {
				return err
			}
			subject, err := enc.NewHead(ctx, vision.SubjectClasses)
			if err != nil {
				return err
			}
			// hand-labelled crops: evaluation set and few-shot prototypes
			var labelled []vision.Label
			var labelledEmb [][]float32
			if labels != "" {
				if labelled, err = vision.LoadLabels(labels); err != nil {
					return err
				}
				imgs := make([]image.Image, 0, len(labelled))
				keep := labelled[:0]
				for _, l := range labelled {
					if img := loadImage(filepath.Join(cfg.DataDir, filepath.FromSlash(l.Path))); img != nil {
						imgs = append(imgs, img)
						keep = append(keep, l)
					}
				}
				labelled = keep
				if labelledEmb, err = enc.Images(ctx, imgs); err != nil {
					return err
				}
				fmt.Printf("%d labelled crops embedded\n", len(labelled))
			}
			if eval {
				return runEval(medium, subject, labelled, labelledEmb)
			}
			if len(labelled) > 0 && mix > 0 {
				med, sub := make([]string, len(labelled)), make([]string, len(labelled))
				for i, l := range labelled {
					med[i], sub[i] = l.Medium, vision.MergeSubject(l.Subject)
				}
				medium.TrainProbe(labelledEmb, med, mix)
				subject.TrainProbe(labelledEmb, sub, mix)
			}
			if len(show) > 0 {
				for _, p := range show {
					img := loadImage(p)
					if img == nil {
						fmt.Printf("%s: cannot read\n", p)
						continue
					}
					embs, err := enc.Images(ctx, []image.Image{img})
					if err != nil {
						return err
					}
					fmt.Println(p)
					for _, h := range []*vision.Head{medium, subject} {
						_, _, probs := h.Classify(embs[0])
						fmt.Printf("   %s\n", topK(h.Names, probs, 4))
					}
				}
				return nil
			}
			st, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer st.Close()
			classify := func(id int64, emb []float32) store.CropVision {
				k, ks, _ := medium.Classify(emb)
				sj, ss, _ := subject.Classify(emb)
				return store.CropVision{ID: id, Kind: k, Score: ks, Subject: sj, SubScore: ss}
			}
			// --spec: the same pipeline over spec-sheet images (product_images source='spec')
			listRefs, saveVision, streamEmb := st.CropsToEmbed, st.SaveCropVision, st.CropEmbeddings
			if spec {
				listRefs, saveVision, streamEmb = st.SpecImagesToEmbed, st.SaveImageVision, st.ImageEmbeddings
			}
			start := time.Now()
			counts := map[string]int{}
			if reclassify && rank != "" {
				// label mining for a new class: the stored embeddings scored by the medium head,
				// the --top most probable members of --rank printed for a human to look at
				return rankCrops(ctx, medium, rank, top, brand, enc.Model, streamEmb)
			}
			if reclassify {
				// prompts or thresholds changed: re-score stored embeddings, no image work
				var pending []store.CropVision
				n := 0
				flush := func() error {
					if len(pending) == 0 || dry {
						pending = pending[:0]
						return nil
					}
					err := saveVision(ctx, enc.Model, pending)
					pending = pending[:0]
					return err
				}
				err := streamEmb(ctx, brand, enc.Model, func(e store.CropEmbedding) error {
					v := classify(e.ID, e.Emb)
					counts[v.Kind]++
					pending = append(pending, v)
					n++
					if len(pending) >= 500 {
						return flush()
					}
					return nil
				})
				if err != nil {
					return err
				}
				if err := flush(); err != nil {
					return err
				}
				fmt.Printf("reclassified %d crops in %s: %v\n", n, time.Since(start).Round(time.Second), counts)
			} else {
				crops, err := listRefs(ctx, brand, enc.Model, all, limit)
				if err != nil {
					return err
				}
				fmt.Printf("%d images to embed with %s\n", len(crops), enc.Model)
				// decode + pad in parallel, run the model in order, write in batches
				type loaded struct {
					ref store.CropRef
					img image.Image
				}
				in := make(chan store.CropRef)
				out := make(chan loaded, 64)
				var wg sync.WaitGroup
				for w := 0; w < max(2, runtime.NumCPU()/2); w++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for ref := range in {
							img := loadImage(filepath.Join(cfg.DataDir, filepath.FromSlash(ref.Path)))
							if img != nil {
								img = vision.Prepare(img)
							}
							out <- loaded{ref, img}
						}
					}()
				}
				go func() {
					for _, c := range crops {
						select {
						case in <- c:
						case <-ctx.Done():
							close(in)
							return
						}
					}
					close(in)
				}()
				go func() { wg.Wait(); close(out) }()
				var batch []loaded
				var pending []store.CropVision
				done, unreadable := 0, 0
				run := func() error {
					if len(batch) == 0 {
						return nil
					}
					imgs := make([]image.Image, len(batch))
					for i, l := range batch {
						imgs[i] = l.img
					}
					embs, err := enc.Images(ctx, imgs)
					if err != nil {
						return err
					}
					for i, l := range batch {
						v := classify(l.ref.ID, embs[i])
						v.Emb = embs[i]
						counts[v.Kind]++
						if dry {
							fmt.Printf("%-8s %.2f  %-9s %.2f  %s\n", v.Kind, v.Score, v.Subject, v.SubScore, l.ref.Path)
						}
						pending = append(pending, v)
					}
					batch = batch[:0]
					if len(pending) >= 256 && !dry {
						if err := saveVision(ctx, enc.Model, pending); err != nil {
							return err
						}
						pending = pending[:0]
					}
					return nil
				}
				for l := range out {
					if l.img == nil {
						unreadable++
						continue
					}
					batch = append(batch, l)
					done++
					if len(batch) >= vision.BatchSize {
						if err := run(); err != nil {
							return err
						}
					}
					if done%1000 == 0 {
						el := time.Since(start)
						fmt.Printf("  %d/%d  %s  (%.0f ms/crop, ~%s left)\n", done, len(crops), el.Round(time.Second),
							float64(el.Milliseconds())/float64(done), (time.Duration(float64(el) / float64(done) * float64(len(crops)-done))).Round(time.Minute))
					}
				}
				if err := run(); err != nil {
					return err
				}
				if len(pending) > 0 && !dry {
					if err := saveVision(ctx, enc.Model, pending); err != nil {
						return err
					}
				}
				fmt.Printf("done in %s: %v (%d unreadable)\n", time.Since(start).Round(time.Second), counts, unreadable)
			}
			if dry {
				return nil
			}
			if !spec {
				changed, err := st.RecomputeCropKinds(ctx)
				if err != nil {
					return err
				}
				kinds, _, embedded, err := st.VisionStats(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("effective kinds: %v (%d crops carry a vision verdict, %d kinds changed)\n", kinds, embedded, changed)
			}
			fmt.Println("re-picking family photos…")
			if err := st.RefreshFamilyBest(ctx); err != nil {
				return err
			}
			_, err = st.PromoteHD(ctx)
			return err
		},
	}
	cmd.Flags().StringVar(&brand, "brand", "", "only this brand code")
	cmd.Flags().StringVar(&model, "model", vision.DefaultModel, "CLIP checkpoint (clip-vit-base-patch16 | clip-vit-base-patch32)")
	cmd.Flags().StringVar(&labels, "labels", "data/vision_labels.tsv", "hand-labelled crops (few-shot prototypes; --eval set); '' to disable")
	cmd.Flags().Float32Var(&mix, "mix", 0.8, "weight of the linear probe trained on --labels (0 = zero-shot prompts only)")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many crops")
	cmd.Flags().IntVar(&threads, "threads", 0, "ONNX intra-op threads (default: all cores)")
	cmd.Flags().BoolVar(&all, "all", false, "re-embed crops that already have an embedding from this model")
	cmd.Flags().BoolVar(&dry, "dry", false, "print verdicts, write nothing")
	cmd.Flags().BoolVar(&eval, "eval", false, "leave-one-out accuracy on --labels; write nothing")
	cmd.Flags().BoolVar(&reclassify, "reclassify", false, "re-score stored embeddings with the current prompts; no image work")
	cmd.Flags().StringVar(&rank, "rank", "", "with --reclassify: print the crops the medium head finds most likely to be this class (label mining)")
	cmd.Flags().IntVar(&top, "top", 60, "how many crops --rank prints")
	cmd.Flags().BoolVar(&spec, "spec", false, "classify spec-sheet images (product_images source='spec') instead of catalog crops")
	cmd.Flags().BoolVar(&tone, "tone", false, "score the metal tone (silver | gold | bronze | black | white | copper) of every photo crop from its stored embedding; no image work (--mix 0 = prompts only)")
	cmd.Flags().BoolVar(&scene, "scene", false, "score every photo crop as product vs room scene from its stored embedding (catalog_crops.vscene); no image work")
	cmd.Flags().BoolVar(&fit, "fit", false, "score how well each candidate card picture fits its family's name (crop_fit); no image work")
	cmd.Flags().StringSliceVar(&show, "show", nil, "classify these image files and print the top classes")
	return cmd
}

// rankCrops streams stored embeddings and prints the top crops by one medium class's
// probability: "prob<TAB>path", most likely first, for picking hand labels of a new class.
func rankCrops(ctx context.Context, medium *vision.Head, class string, top int, brand, model string,
	stream func(context.Context, string, string, func(store.CropEmbedding) error) error) error {
	ci := -1
	for i, n := range medium.Names {
		if n == class {
			ci = i
		}
	}
	if ci < 0 {
		return fmt.Errorf("--rank %q: not a medium class (%s)", class, strings.Join(medium.Names, ", "))
	}
	type hit struct {
		p    float32
		path string
	}
	var hits []hit
	n := 0
	err := stream(ctx, brand, model, func(e store.CropEmbedding) error {
		n++
		_, _, probs := medium.Classify(e.Emb)
		if len(hits) < top || probs[ci] > hits[len(hits)-1].p {
			hits = append(hits, hit{probs[ci], e.Path})
			sort.Slice(hits, func(a, b int) bool { return hits[a].p > hits[b].p })
			if len(hits) > top {
				hits = hits[:top]
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d embeddings scored\n", n)
	for _, h := range hits {
		fmt.Printf("%.2f\t%s\n", h.p, h.path)
	}
	return nil
}

func topK(names []string, probs []float32, k int) string {
	idx := make([]int, len(names))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return probs[idx[a]] > probs[idx[b]] })
	var parts []string
	for _, i := range idx[:min(k, len(idx))] {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", names[i], 100*probs[i]))
	}
	return strings.Join(parts, "  ")
}

// runEval prints leave-one-out accuracy over a grid of prototype weights for both heads.
func runEval(medium, subject *vision.Head, labelled []vision.Label, embs [][]float32) error {
	if len(labelled) == 0 {
		return fmt.Errorf("no labelled crops")
	}
	paths := make([]string, len(labelled))
	med := make([]string, len(labelled))
	sub := make([]string, len(labelled))
	for i, l := range labelled {
		paths[i], med[i], sub[i] = l.Path, l.Medium, vision.MergeSubject(l.Subject)
	}
	for _, h := range []struct {
		name  string
		head  *vision.Head
		truth []string
	}{{"medium", medium, med}, {"subject", subject, sub}} {
		fmt.Printf("\n== %s head (%d labelled)\n", h.name, len(labelled))
		var best vision.Eval
		for _, temp := range []float32{10, 20, 50} {
			for _, mix := range []float32{0, 0.3, 0.5, 0.7, 1} {
				if mix == 0 && temp != 10 {
					continue
				}
				ev := h.head.LeaveOneOut(embs, h.truth, paths, mix, temp)
				fmt.Printf("  mix=%.1f protoTemp=%-3.0f  %.1f%% (%d/%d)\n", mix, temp, 100*ev.Accuracy(), ev.Correct, ev.Total)
				if ev.Correct > best.Correct {
					best = ev
				}
			}
		}
		for _, w := range []float32{0.5, 0.8, 1} {
			ev := h.head.CrossValidate(embs, h.truth, paths, 10, w, 1)
			fmt.Printf("  probe(10-fold) weight=%.1f     %.1f%% (%d/%d)\n", w, 100*ev.Accuracy(), ev.Correct, ev.Total)
			if ev.Correct > best.Correct {
				best = ev
				best.ProtoTemp = -1
			}
		}
		fmt.Printf("  best: mix=%.1f protoTemp=%.0f (−1 = linear probe)  %.1f%%\n", best.Mix, best.ProtoTemp, 100*best.Accuracy())
		fmt.Println("  confidence ≥ t: right / wrong (coverage)")
		for _, t := range vision.Thresholds {
			b := best.Buckets[t]
			fmt.Printf("    ≥%.1f  %4d / %-4d (%.0f%% of %d, precision %.1f%%)\n", t, b[0], b[1], 100*float64(b[0]+b[1])/float64(best.Total), best.Total, 100*float64(b[0])/float64(max(1, b[0]+b[1])))
		}
		fmt.Println("  confusion (truth→pred):")
		keys := make([][2]string, 0, len(best.Confusion))
		for k := range best.Confusion {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a][0] != keys[b][0] {
				return keys[a][0] < keys[b][0]
			}
			return best.Confusion[keys[a]] > best.Confusion[keys[b]]
		})
		for _, k := range keys {
			mark := ""
			if k[0] != k[1] {
				mark = "  ✗"
			}
			fmt.Printf("    %-9s → %-9s %4d%s\n", k[0], k[1], best.Confusion[k], mark)
		}
		fmt.Println("  misses:")
		for _, m := range best.Misses {
			fmt.Println("    " + m)
		}
	}
	return nil
}

var _ = context.Background
var _ = os.Open

// runTone scores every photo crop's metal tone from its stored embedding: zero-shot prompts
// mixed with prototypes from crops linked to SKUs of a known finish (and a linear probe on
// them when there are enough). Vendor images take the tone of their SKU's finish directly.
func runTone(ctx context.Context, dbURL string, enc *vision.Encoder, brand string, mix float32, dry bool) error {
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	head, err := enc.NewHead(ctx, vision.ToneClasses)
	if err != nil {
		return err
	}
	ex, err := st.ToneExamples(ctx, enc.Model, 400)
	if err != nil {
		return err
	}
	byClass := map[string][][]float32{}
	var embs [][]float32
	var labels []string
	for _, e := range ex {
		byClass[e.Group] = append(byClass[e.Group], e.Emb)
		embs = append(embs, e.Emb)
		labels = append(labels, e.Group)
	}
	counts := map[string]int{}
	for k, v := range byClass {
		counts[k] = len(v)
	}
	// vendor images of a known finish are the check set: zero-shot accuracy first, then with
	// their prototypes mixed in (held out five ways) when --mix asks for it
	zero := 0
	confusion := map[string]map[string]int{}
	for i, e := range embs {
		t, _, _ := head.Classify(e)
		if t == labels[i] {
			zero++
		}
		if confusion[labels[i]] == nil {
			confusion[labels[i]] = map[string]int{}
		}
		confusion[labels[i]][t]++
	}
	fmt.Printf("%d vendor images of a known finish: %v\n", len(ex), counts)
	if len(ex) > 0 {
		fmt.Printf("zero-shot accuracy on them: %.1f%%\n", 100*float64(zero)/float64(len(ex)))
		for truth, row := range confusion {
			fmt.Printf("  %-7s → %v\n", truth, row)
		}
	}
	if len(ex) >= 50 && mix > 0 {
		head.SetPrototypes(byClass, mix)
		ev := head.CrossValidate(embs, labels, make([]string, len(embs)), 5, 0, 1)
		fmt.Printf("5-fold accuracy with prototypes (mix %.1f): %.1f%%\n", mix, 100*ev.Accuracy())
	}
	start := time.Now()
	verdicts := map[string]int{}
	var pending []store.CropTone
	n := 0
	flush := func() error {
		if len(pending) == 0 || dry {
			pending = pending[:0]
			return nil
		}
		err := st.SaveCropTones(ctx, pending)
		pending = pending[:0]
		return err
	}
	err = st.PhotoEmbeddings(ctx, brand, enc.Model, func(e store.CropEmbedding) error {
		t, sc, _ := head.Classify(e.Emb)
		verdicts[t]++
		if dry && n < 40 {
			fmt.Printf("%-7s %.2f  %s\n", t, sc, e.Path)
		}
		pending = append(pending, store.CropTone{ID: e.ID, Tone: t, Score: sc})
		n++
		if len(pending) >= 500 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	fmt.Printf("toned %d crops in %s: %v\n", n, time.Since(start).Round(time.Second), verdicts)
	if dry {
		return nil
	}
	set, err := st.RefreshImageTones(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("vendor images toned from their SKU finish: %d\n", set)
	n2, err := st.RefreshFamilyTones(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("family card tones refreshed: %d changed\n", n2)
	return nil
}
