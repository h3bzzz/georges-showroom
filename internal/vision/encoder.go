//go:build ORT

package vision

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"runtime"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/backends"
	"github.com/knights-analytics/hugot/options"
	"github.com/knights-analytics/hugot/pipelines"
	"github.com/knights-analytics/hugot/util/imageutil"
	xdraw "golang.org/x/image/draw"
)

// Side is the CLIP input resolution.
const Side = 224

// BatchSize is how many images go through the vision tower per ONNX call.
const BatchSize = 16

// Options for Open.
type Options struct {
	DataDir string
	Model   string // key of Models; DefaultModel when empty
	Threads int    // intra-op threads; NumCPU when 0
}

// Encoder holds an ORT session with the CLIP vision and text towers.
type Encoder struct {
	Model string
	Dim   int
	sess  *hugot.Session
	img   *pipelines.ImageFeatureExtractionPipeline
	txt   *pipelines.FeatureExtractionPipeline
}

// Open installs the runtime and model on first use and loads both towers.
func Open(ctx context.Context, o Options) (*Encoder, error) {
	if o.Model == "" {
		o.Model = DefaultModel
	}
	if o.Threads <= 0 {
		o.Threads = runtime.NumCPU()
	}
	libDir, err := EnsureRuntime(ctx, o.DataDir)
	if err != nil {
		return nil, err
	}
	modelDir, err := EnsureModel(ctx, o.DataDir, o.Model)
	if err != nil {
		return nil, err
	}
	sess, err := hugot.NewORTSession(ctx,
		options.WithOnnxLibraryPath(libDir),
		options.WithIntraOpNumThreads(o.Threads),
		options.WithLogSeverityLevel(options.LoggingLevelError))
	if err != nil {
		return nil, fmt.Errorf("onnxruntime session: %w", err)
	}
	e := &Encoder{Model: o.Model, sess: sess}
	// CLIP normalisation (preprocessor_config.json); pixels arrive 0–255 from hugot
	norm := imageutil.PixelNormalizationStep(
		[3]float32{0.48145466 * 255, 0.4578275 * 255, 0.40821073 * 255},
		[3]float32{0.26862954 * 255, 0.26130258 * 255, 0.27577711 * 255})
	e.img, err = hugot.NewPipeline(sess, backends.PipelineConfig[*pipelines.ImageFeatureExtractionPipeline]{
		ModelPath: modelDir, Name: "clip-vision", OnnxFilename: "vision_model.onnx",
		Options: []backends.PipelineOption[*pipelines.ImageFeatureExtractionPipeline]{
			selectOutput("image_embeds"),
			pipelines.WithImageEmbeddingNormalization(),
			pipelines.WithPreprocessSteps[*pipelines.ImageFeatureExtractionPipeline](prepareStep{}),
			pipelines.WithNormalizationSteps[*pipelines.ImageFeatureExtractionPipeline](norm),
		}})
	if err != nil {
		sess.Destroy()
		return nil, fmt.Errorf("vision tower: %w", err)
	}
	e.txt, err = hugot.NewPipeline(sess, backends.PipelineConfig[*pipelines.FeatureExtractionPipeline]{
		ModelPath: modelDir, Name: "clip-text", OnnxFilename: "text_model.onnx",
		Options: []backends.PipelineOption[*pipelines.FeatureExtractionPipeline]{
			pipelines.WithOutputName("text_embeds"), pipelines.WithNormalization()}})
	if err != nil {
		sess.Destroy()
		return nil, fmt.Errorf("text tower: %w", err)
	}
	// hugot sizes the input_ids tensor from the attention mask; a model without an
	// attention_mask input gets a mask-less tokenisation and only its BOS token is fed, so
	// every prompt embeds identically. Ask for the mask anyway; it is never sent to the model.
	if tk := e.txt.Model.Tokenizer; tk != nil && tk.GoTokenizer != nil {
		tk.GoTokenizer.AttentionMask = true
	}
	for _, out := range e.img.Model.OutputsMeta {
		if out.Name == "image_embeds" && len(out.Dimensions) == 2 {
			e.Dim = int(out.Dimensions[1])
		}
	}
	return e, nil
}

// Close releases the ORT session.
func (e *Encoder) Close() error {
	if e == nil || e.sess == nil {
		return nil
	}
	return e.sess.Destroy()
}

// selectOutput picks a named model output (hugot's image pipeline defaults to the token
// sequence; CLIP's projected image_embeds is what the text tower is aligned with).
func selectOutput(name string) backends.PipelineOption[*pipelines.ImageFeatureExtractionPipeline] {
	return func(p *pipelines.ImageFeatureExtractionPipeline) error {
		for i, o := range p.Model.OutputsMeta {
			if o.Name == name {
				p.OutputIndex = i
				return nil
			}
		}
		return fmt.Errorf("model has no %s output", name)
	}
}

// Texts embeds prompts (unit vectors).
func (e *Encoder) Texts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out, err := e.txt.RunPipeline(ctx, texts)
	if err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// Images embeds images (unit vectors), BatchSize at a time.
func (e *Encoder) Images(ctx context.Context, imgs []image.Image) ([][]float32, error) {
	var all [][]float32
	for i := 0; i < len(imgs); i += BatchSize {
		j := min(len(imgs), i+BatchSize)
		out, err := e.img.RunWithImages(ctx, imgs[i:j])
		if err != nil {
			return nil, err
		}
		all = append(all, out.Embeddings...)
	}
	return all, nil
}

// prepareStep adapts Prepare to hugot's preprocess interface.
type prepareStep struct{}

func (prepareStep) Apply(img image.Image) (image.Image, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, fmt.Errorf("empty image")
	}
	return Prepare(img), nil
}

// Prepare pads an image to a square on white (catalog pages are white, and a centre crop
// would cut the spout off a tall faucet) and scales it to Side×Side with a Catmull-Rom filter.
func Prepare(img image.Image) *image.RGBA {
	b := img.Bounds()
	side := max(b.Dx(), b.Dy())
	sq := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(sq, sq.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	off := image.Pt((side-b.Dx())/2, (side-b.Dy())/2)
	draw.Draw(sq, image.Rectangle{Min: off, Max: off.Add(b.Size())}, img, b.Min, draw.Over)
	dst := image.NewRGBA(image.Rect(0, 0, Side, Side))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), sq, sq.Bounds(), xdraw.Src, nil)
	return dst
}

// Dot is the cosine similarity of two unit vectors.
func Dot(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Normalize scales v to unit length in place and returns it.
func Normalize(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	k := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= k
	}
	return v
}

// Mean averages vectors and normalises the result.
func Mean(vs [][]float32) []float32 {
	if len(vs) == 0 {
		return nil
	}
	m := make([]float32, len(vs[0]))
	for _, v := range vs {
		for i := range m {
			m[i] += v[i]
		}
	}
	return Normalize(m)
}
