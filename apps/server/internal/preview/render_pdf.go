package preview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
)

// pdfBinary is poppler's rasterizer, shipped by the poppler-utils package.
const pdfBinary = "pdftoppm"

// PDFRenderer rasterizes the first page of a PDF.
//
// pdftoppm parses attacker-supplied documents in a memory-unsafe C stack with
// a long CVE history, which is why it runs in cmd/worker's container rather
// than next to the API's session key and connection pool.
type PDFRenderer struct{}

// NewPDFRenderer returns a renderer, or nil if poppler is not installed.
// A nil renderer is dropped by the caller, so the process simply never
// advertises render:pdf and those jobs wait for a worker that has the binary.
func NewPDFRenderer() Renderer {
	if !hasBinary(pdfBinary) {
		return nil
	}
	return PDFRenderer{}
}

func (PDFRenderer) Kind() string { return KindRenderPDF }

func (PDFRenderer) Render(ctx context.Context, src io.Reader, _ Payload) ([]byte, error) {
	path, cleanup, err := spool(src, "in.pdf")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// -f/-l 1     first page only
	// -singlefile drop the "-000001" page suffix pdftoppm would append
	// -scale-to-x oversample to 512 so the downscale in imageThumbnail has
	//             detail to work with; -1 keeps the aspect ratio
	out := filepath.Join(filepath.Dir(path), "out")
	err = runQuiet(ctx, pdfBinary,
		"-png", "-f", "1", "-l", "1", "-singlefile",
		"-scale-to-x", "512", "-scale-to-y", "-1",
		path, out,
	)
	if err != nil {
		return nil, err
	}

	raster, err := readCapped(out+".png", maxRenderOutput)
	if err != nil {
		return nil, fmt.Errorf("read rasterized page: %w", err)
	}
	// Funnel through imageThumbnail so the 256px cap lives in one place.
	return imageThumbnail(bytes.NewReader(raster))
}
