package preview

import (
	"bytes"
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
	thumbMaxDim   = 256
	textMaxBytes  = 4096
	pdfPagePrefix = "%PDF"
)

// imageThumbnail scales an image to fit within thumbMaxDim and re-encodes as PNG.
func imageThumbnail(r io.Reader) ([]byte, error) {
	src, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := float64(thumbMaxDim) / float64(max(w, h))
	if scale >= 1 {
		scale = 1 // don't upscale
	}
	dst := image.NewRGBA(image.Rect(0, 0, int(float64(w)*scale), int(float64(h)*scale)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pdfFirstPage returns the first N bytes of a PDF as a placeholder preview.
// Full rasterization needs pdfium/pdftoppm — flagged as a post-M5 job kind.
func pdfFirstPage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, 64))
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte(pdfPagePrefix)) {
		return nil, fmt.Errorf("not a pdf")
	}
	// MVP: return a text marker; real PDF rasterization is a follow-up.
	return []byte("PDF document — preview placeholder"), nil
}

// videoProbe returns a text summary. Real frame extraction needs ffmpeg — post-M5.
func videoProbe(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, 32))
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("video file (%d bytes probed)", len(data))), nil
}

// textSnippet returns the first textMaxBytes of a text file.
func textSnippet(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, textMaxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}
