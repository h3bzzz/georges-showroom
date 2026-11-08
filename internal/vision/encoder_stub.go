//go:build !ORT || !cgo

package vision

import (
	"context"
	"image"
)

// Side is the CLIP input resolution.
const Side = 224

// BatchSize is how many images go through the vision tower per ONNX call.
const BatchSize = 16

// Options for Open.
type Options struct {
	DataDir string
	Model   string
	Threads int
}

// Encoder is unavailable in this build.
type Encoder struct {
	Model string
	Dim   int
}

// Open reports that the encoder is not built in.
func Open(context.Context, Options) (*Encoder, error) { return nil, ErrUnavailable }

func (e *Encoder) Close() error                                       { return nil }
func (e *Encoder) Texts(context.Context, []string) ([][]float32, error) { return nil, ErrUnavailable }
func (e *Encoder) Images(context.Context, []image.Image) ([][]float32, error) {
	return nil, ErrUnavailable
}
