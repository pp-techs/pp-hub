package preview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"

	// Register image formats.
	_ "image/gif"
	_ "image/jpeg"

	"golang.org/x/image/draw"
)

const (
	thumbMaxDim  = 256
	textMaxBytes = 4096
)

// ImageRenderer thumbnails raster images. Pure Go, so the API server itself
// registers it — no external binary, nothing to sandbox.
type ImageRenderer struct{}

func (ImageRenderer) Kind() string { return KindRenderImage }

func (ImageRenderer) Render(_ context.Context, src io.Reader, _ Payload) ([]byte, error) {
	return imageThumbnail(src)
}

// TextRenderer takes the head of a text file. Also pure Go.
type TextRenderer struct{}

func (TextRenderer) Kind() string { return KindSnippetText }

func (TextRenderer) Render(_ context.Context, src io.Reader, _ Payload) ([]byte, error) {
	return io.ReadAll(io.LimitReader(src, textMaxBytes))
}

// imageThumbnail scales an image to fit within thumbMaxDim and re-encodes as
// PNG. Every renderer that rasterizes funnels through here, so the size cap
// and encoding are defined exactly once.
func imageThumbnail(r io.Reader) ([]byte, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty image %dx%d", w, h)
	}
	scale := float64(thumbMaxDim) / float64(max(w, h))
	if scale >= 1 {
		scale = 1 // don't upscale
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
