package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"georges_/internal/store"
	"georges_/internal/vision"
)

// runScene scores every photo crop's probability of being a room interior or mood shot
// rather than a product photograph, from its stored embedding (no image work).
func runScene(ctx context.Context, dbURL string, enc *vision.Encoder, brand string, dry bool) error {
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	head, err := enc.NewHead(ctx, vision.SceneClasses)
	if err != nil {
		return err
	}
	start := time.Now()
	var pending []store.CropScene
	n, scenes := 0, 0
	flush := func() error {
		if len(pending) == 0 || dry {
			pending = pending[:0]
			return nil
		}
		err := st.SaveCropScenes(ctx, pending)
		pending = pending[:0]
		return err
	}
	err = st.PhotoEmbeddings(ctx, brand, enc.Model, func(e store.CropEmbedding) error {
		probs := head.Probs(e.Emb)
		var scene float32
		for i, c := range vision.SceneClasses {
			if c.Name == "scene" {
				scene = probs[i]
			}
		}
		if scene >= 0.5 {
			scenes++
			if dry && scenes <= 40 {
				fmt.Printf("scene %.2f  %s\n", scene, e.Path)
			}
		}
		pending = append(pending, store.CropScene{ID: e.ID, Scene: scene})
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
	fmt.Printf("scored %d photo crops in %s: %d read as a room scene (≥0.5)\n", n, time.Since(start).Round(time.Second), scenes)
	return nil
}

// runFit scores how well each candidate card picture fits the family it would stand for:
// cosine between the CLIP text embedding of the family's name ("a photo of a Toilet Brush
// Set, a bath accessory") and the crop's stored image embedding, minus the crop's cosine with
// a generic product sentence, so a crop that resembles everything scores nothing. Stored in
// crop_fit; RefreshFamilyBest ranks candidates by it and refuses series openers below the
// floor.
func runFit(ctx context.Context, dbURL string, enc *vision.Encoder, brand string, dry bool) error {
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	base, err := enc.Texts(ctx, []string{"a photo of a kitchen or bathroom product"})
	if err != nil {
		return err
	}
	generic := vision.Normalize(base[0])
	start := time.Now()
	var batch []store.FitFamily
	var pending []store.CropFit
	fams, pairs := 0, 0
	var shown int
	hist := map[int]int{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		texts := make([]string, len(batch))
		for i, f := range batch {
			texts[i] = store.FitText(f.Title, f.Category)
		}
		embs, err := enc.Texts(ctx, texts)
		if err != nil {
			return err
		}
		for i, f := range batch {
			te := vision.Normalize(embs[i])
			best := float32(-1)
			var bestSrc string
			for _, c := range f.Candidates {
				ce := vision.Normalize(c.Emb)
				fit := vision.Dot(te, ce) - vision.Dot(generic, ce)
				pending = append(pending, store.CropFit{FamilyID: f.ID, CropID: c.CropID, Fit: fit})
				hist[int(fit*100)/2*2]++
				if fit > best {
					best, bestSrc = fit, c.Source
				}
				pairs++
			}
			if dry && shown < 30 {
				fmt.Printf("%-60.60s best %.3f (%s, %d candidates)\n", texts[i], best, bestSrc, len(f.Candidates))
				shown++
			}
		}
		batch = batch[:0]
		if dry {
			pending = pending[:0]
			return nil
		}
		if len(pending) >= 1000 {
			err := st.SaveCropFits(ctx, pending)
			pending = pending[:0]
			return err
		}
		return nil
	}
	err = st.FitCandidates(ctx, brand, enc.Model, func(f store.FitFamily) error {
		batch = append(batch, f)
		fams++
		if len(batch) >= 64 {
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
	if !dry && len(pending) > 0 {
		if err := st.SaveCropFits(ctx, pending); err != nil {
			return err
		}
	}
	keys := make([]int, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	fmt.Printf("fit scored for %d families, %d candidate pairs in %s\n", fams, pairs, time.Since(start).Round(time.Second))
	for _, k := range keys {
		fmt.Printf("  %+.2f  %d\n", float64(k)/100, hist[k])
	}
	if dry {
		return nil
	}
	n, err := st.RefreshPhotoFit(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("family_best.photo_fit set for %d cards\n", n)
	return nil
}
