package preview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"strings"

	// Register image formats.
	_ "image/gif"
	_ "image/jpeg"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

const (
	thumbMaxDim  = 256
	textMaxBytes = 4096

	// defaultSVGWidth/Height are the CSS replaced-element default (300×150)
	// an <svg> root falls back to when it declares neither a viewBox nor
	// explicit width/height — matches what a browser would size it to.
	defaultSVGWidth  = 300
	defaultSVGHeight = 150

	// maxImagePixels is about 67 MP, above any phone's full-resolution mode;
	// decode memory is 4–8 bytes per pixel, so this bounds a decode near 512 MiB.
	maxImagePixels = 64 << 20
	// maxSVGBytes bounds the SVG source read into memory before parsing.
	maxSVGBytes = 8 << 20
)

// ImageRenderer thumbnails raster images. Pure Go, so the API server itself
// registers it — no external binary, nothing to sandbox.
type ImageRenderer struct{}

func (ImageRenderer) Kind() string { return KindRenderImage }

func (ImageRenderer) Render(_ context.Context, src io.Reader, _ Payload) ([]byte, error) {
	return imageThumbnail(src)
}

// SVGRenderer thumbnails SVG images by rasterizing them with oksvg/rasterx —
// both pure Go, so like ImageRenderer it runs in the API server itself with
// no external binary. Rasterizing rather than serving the markup is also what
// keeps a thumbnail request safe: the renderer only ever walks paths and
// paints pixels onto an RGBA buffer, so an embedded <script> or an external
// href never executes or gets fetched, unlike pointing a browser at the raw
// file (why /content downgrades image/svg+xml to attachment — see drive.go).
type SVGRenderer struct{}

func (SVGRenderer) Kind() string { return KindRenderSVG }

func (SVGRenderer) Render(_ context.Context, src io.Reader, _ Payload) ([]byte, error) {
	return svgThumbnail(src)
}

// TextRenderer takes the head of a text file. Also pure Go.
type TextRenderer struct{}

func (TextRenderer) Kind() string { return KindSnippetText }

func (TextRenderer) Render(_ context.Context, src io.Reader, _ Payload) ([]byte, error) {
	return io.ReadAll(io.LimitReader(src, textMaxBytes))
}

// svgThumbnail rasterizes an SVG to fit within thumbMaxDim and encodes it as
// PNG. Scaling happens in the rasterizer via SetTarget rather than by
// resampling afterwards, so a vector source renders straight at the target
// resolution instead of through an extra downscale pass.
//
// oksvg's color parser only understands literal values (hex, rgb, named) —
// it hard-errors on the bare token currentColor and on any var()/color-mix()
// call, even a var() naming a plain hex color declared right there in the
// SVG. Both are common: currentColor is how icon sets like Feather/Lucide
// theme fill/stroke, and var()-of-root-custom-property is how generated
// chart/diagram exports do the same. resolveCSSColors (svg_colors.go) does a
// best-effort textual substitution for both before parsing, so those files
// get a thumbnail — in the right color when resolvable, in svgFallbackColor
// when it is genuinely computed (color-mix) or otherwise out of reach —
// instead of failing to render at all.
func svgThumbnail(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSVGBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read svg: %w", err)
	}
	if len(data) > maxSVGBytes {
		return nil, fmt.Errorf("svg is over the %d byte limit", maxSVGBytes)
	}
	svg := string(data)
	if strings.Contains(svg, "currentColor") {
		svg = strings.ReplaceAll(svg, "currentColor", svgFallbackColor)
	}
	if strings.Contains(svg, "var(") || strings.Contains(svg, "color-mix(") {
		svg = resolveCSSColors(svg)
	}
	// IgnoreErrorMode, not Warn: oksvg's warning path writes straight to the
	// stdlib global log package on stderr, once per unsupported element (most
	// commonly <text> — oksvg draws shapes, not text layout). A drive full of
	// generated diagrams would flood the server's log with an unstructured
	// line per label on every render; skipping silently is the right default
	// since the renderer already degrades gracefully (missing element, not
	// missing file).
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}
	vw, vh := icon.ViewBox.W, icon.ViewBox.H
	if vw <= 0 || vh <= 0 {
		vw, vh = defaultSVGWidth, defaultSVGHeight
	}
	scale := float64(thumbMaxDim) / max(vw, vh)
	if scale >= 1 {
		scale = 1 // don't upscale
	}
	w, h := max(int(vw*scale), 1), max(int(vh*scale), 1)
	icon.SetTarget(0, 0, float64(w), float64(h))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, dst, dst.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1.0)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// imageThumbnail scales an image to fit within thumbMaxDim and re-encodes as
// PNG. Every renderer that rasterizes funnels through here, so the size cap
// and encoding are defined exactly once.
func imageThumbnail(r io.Reader) ([]byte, error) {
	var head bytes.Buffer
	cfg, _, err := image.DecodeConfig(io.TeeReader(r, &head))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("empty image %dx%d", cfg.Width, cfg.Height)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("image %dx%d is over the %d pixel limit", cfg.Width, cfg.Height, maxImagePixels)
	}
	src, _, err := image.Decode(io.MultiReader(&head, r))
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
