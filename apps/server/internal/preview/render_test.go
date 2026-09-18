package preview

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A renderer constructor returns nil when its binary is absent, which is how a
// process ends up advertising only what it can run. Asserted both ways so the
// test is meaningful on a slim CI image and on a full worker image alike.
func TestRendererConstructorsFollowBinaryAvailability(t *testing.T) {
	cases := []struct {
		name string
		got  Renderer
		bins []string
	}{
		{"pdf", NewPDFRenderer(), []string{pdfBinary}},
		{"video", NewVideoRenderer(), []string{videoBinary, probeBinary}},
	}
	for _, c := range cases {
		available := true
		for _, b := range c.bins {
			if !hasBinary(b) {
				available = false
			}
		}
		switch {
		case available && c.got == nil:
			t.Errorf("%s: binaries present but constructor returned nil", c.name)
		case !available && c.got != nil:
			t.Errorf("%s: binaries missing but constructor returned a renderer", c.name)
		}
	}
}

// decodeThumb asserts the bytes are a PNG within the size cap.
func decodeThumb(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("output is not a valid PNG: %v", err)
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w > thumbMaxDim || h > thumbMaxDim {
		t.Errorf("thumbnail is %dx%d, over the %dpx cap", w, h, thumbMaxDim)
	}
	return img
}

func TestImageRendererDownscales(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for x := range 1000 {
		src.Set(x, 10, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	out, err := (ImageRenderer{}).Render(t.Context(), &buf, Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	// Aspect ratio must survive: 1000x500 capped at 256 gives 256x128.
	if got := img.Bounds().Dx(); got != thumbMaxDim {
		t.Errorf("width = %d, want %d", got, thumbMaxDim)
	}
	if got := img.Bounds().Dy(); got != thumbMaxDim/2 {
		t.Errorf("height = %d, want %d", got, thumbMaxDim/2)
	}
}

// Small images must not be blown up to fill the cap.
func TestImageRendererDoesNotUpscale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 32))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := (ImageRenderer{}).Render(t.Context(), &buf, Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 32 {
		t.Errorf("got %v, want 64x32 unchanged", img.Bounds())
	}
}

func TestSVGRendererDownscales(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 500">
		<rect width="1000" height="500" fill="#ff0000"/>
	</svg>`
	out, err := (SVGRenderer{}).Render(t.Context(), strings.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	// Aspect ratio must survive: 1000x500 capped at 256 gives 256x128.
	if got := img.Bounds().Dx(); got != thumbMaxDim {
		t.Errorf("width = %d, want %d", got, thumbMaxDim)
	}
	if got := img.Bounds().Dy(); got != thumbMaxDim/2 {
		t.Errorf("height = %d, want %d", got, thumbMaxDim/2)
	}
}

// currentColor is how icon sets like Feather/Lucide theme fill/stroke via
// CSS; oksvg's color parser errors on the literal token, so without the
// substitution in svgThumbnail every such icon would fail to render.
func TestSVGRendererHandlesCurrentColor(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor">
		<path d="M23 19a2 2 0 0 1-2 2H3a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4l2-3h6l2 3h4a2 2 0 0 1 2 2z"/>
		<circle cx="12" cy="13" r="4"/>
	</svg>`
	out, err := (SVGRenderer{}).Render(t.Context(), strings.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	decodeThumb(t, out)
}

// Generated chart/diagram exports theme via a handful of custom properties
// on the root <svg style="...">, referenced with var() from presentation
// attributes — the pattern that made every SVG in a real drive fail before
// resolveCSSColors existed. This renders the real shape in the declared
// color rather than the svgFallbackColor collapseColorFuncs would use for
// something genuinely unresolvable.
func TestSVGRendererHandlesRootCustomProperties(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" style="--fill:#00ff00">
		<rect width="10" height="10" fill="var(--fill)"/>
	</svg>`
	out, err := (SVGRenderer{}).Render(t.Context(), strings.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
	want := color.RGBA{G: 255}
	if got != want {
		t.Errorf("center pixel = %v, want %v (the declared --fill, not svgFallbackColor)", got, want)
	}
}

// Small SVGs must not be blown up to fill the cap, matching ImageRenderer.
func TestSVGRendererDoesNotUpscale(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 32">
		<rect width="64" height="32" fill="#00ff00"/>
	</svg>`
	out, err := (SVGRenderer{}).Render(t.Context(), strings.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 32 {
		t.Errorf("got %v, want 64x32 unchanged", img.Bounds())
	}
}

// An <svg> root with neither a viewBox nor width/height falls back to the
// CSS replaced-element default (300×150) instead of dividing by zero, then
// still scales that default down to the cap like any other size.
func TestSVGRendererDefaultsMissingViewport(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	out, err := (SVGRenderer{}).Render(t.Context(), strings.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	wantW, wantH := thumbMaxDim, defaultSVGHeight*thumbMaxDim/defaultSVGWidth
	if img.Bounds().Dx() != wantW || img.Bounds().Dy() != wantH {
		t.Errorf("got %v, want %dx%d", img.Bounds(), wantW, wantH)
	}
}

func TestTextRendererTruncates(t *testing.T) {
	src := bytes.Repeat([]byte("a"), textMaxBytes*2)
	out, err := (TextRenderer{}).Render(t.Context(), bytes.NewReader(src), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out) != textMaxBytes {
		t.Errorf("got %d bytes, want %d", len(out), textMaxBytes)
	}
}

// makeVideo writes a short synthetic clip with ffmpeg.
func makeVideo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command(videoBinary,
		"-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=640x360:rate=10",
		"-pix_fmt", "yuv420p", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v: %s", err, out)
	}
	return path
}

func TestVideoRendererGrabsFrame(t *testing.T) {
	r := NewVideoRenderer()
	if r == nil {
		t.Skipf("%s/%s not installed", videoBinary, probeBinary)
	}
	f, err := os.Open(makeVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	out, err := r.Render(t.Context(), f, Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeThumb(t, out)
	// 640x360 capped at 256 gives 256x144.
	if img.Bounds().Dx() != thumbMaxDim {
		t.Errorf("width = %d, want %d", img.Bounds().Dx(), thumbMaxDim)
	}
}

// A truncated file must fail cleanly rather than hang or panic: the job then
// retries and eventually lands in 'failed' with a readable last_error.
func TestVideoRendererRejectsGarbage(t *testing.T) {
	r := NewVideoRenderer()
	if r == nil {
		t.Skipf("%s/%s not installed", videoBinary, probeBinary)
	}
	_, err := r.Render(t.Context(), bytes.NewReader([]byte("this is not a video")), Payload{})
	if err == nil {
		t.Fatal("expected an error for non-video input")
	}
}

// minimalPDF is a hand-built single-page document — enough for pdftoppm to
// rasterize without pulling in a PDF library just for a fixture.
const minimalPDF = `%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 100]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj
4 0 obj<</Length 44>>stream
BT /F1 24 Tf 20 40 Td (pp-hub) Tj ET
endstream
endobj
5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj
trailer<</Root 1 0 R>>
`

func TestPDFRendererRasterizesFirstPage(t *testing.T) {
	r := NewPDFRenderer()
	if r == nil {
		t.Skipf("%s not installed (poppler-utils)", pdfBinary)
	}
	out, err := r.Render(t.Context(), bytes.NewReader([]byte(minimalPDF)), Payload{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	decodeThumb(t, out)
}

func TestPDFRendererRejectsGarbage(t *testing.T) {
	r := NewPDFRenderer()
	if r == nil {
		t.Skipf("%s not installed (poppler-utils)", pdfBinary)
	}
	if _, err := r.Render(t.Context(), bytes.NewReader([]byte("not a pdf")), Payload{}); err == nil {
		t.Fatal("expected an error for non-PDF input")
	}
}

// An empty upload must not reach a renderer binary at all.
func TestSpoolRejectsEmptyInput(t *testing.T) {
	_, cleanup, err := spool(bytes.NewReader(nil), "in.bin")
	if err == nil {
		cleanup()
		t.Fatal("expected an error for empty input")
	}
}

func TestRunCaptureTimesOut(t *testing.T) {
	if !hasBinary("sleep") {
		t.Skip("sleep not available")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already-cancelled context stands in for the deadline
	if _, err := runCapture(ctx, "sleep", "60"); err == nil {
		t.Fatal("expected an error when the context is done")
	}
}

// The output cap must bite while the child is writing, not after it exits.
// Checking afterwards means the memory has already been allocated, so a
// renderer coaxed into emitting gigabytes takes the worker down first.
func TestRunCaptureCapsOutputDuringWrite(t *testing.T) {
	if !hasBinary("sh") {
		t.Skip("sh not available")
	}
	// yes(1) emits without bound; the cap has to stop it.
	_, err := runCapture(t.Context(), "sh", "-c", "yes pp-hub")
	if err == nil {
		t.Fatal("expected unbounded output to be refused")
	}
}

func TestCappedBufferRefusesOversizeWrite(t *testing.T) {
	c := &cappedBuffer{max: 8}
	if _, err := c.Write([]byte("12345678")); err != nil {
		t.Fatalf("write at the cap should succeed: %v", err)
	}
	if _, err := c.Write([]byte("9")); err == nil {
		t.Error("write past the cap should fail")
	}
	if c.buf.Len() != 8 {
		t.Errorf("buffer grew to %d, want 8", c.buf.Len())
	}
}

// pdftoppm rasterizes to disk, so its output bypasses the stdout cap entirely
// and needs its own bound.
func TestReadCappedRejectsOversizeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCapped(path, 50); err == nil {
		t.Error("expected a file over the cap to be refused")
	}
	got, err := readCapped(path, 100)
	if err != nil {
		t.Fatalf("a file exactly at the cap must be allowed: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("read %d bytes, want 100", len(got))
	}
}

// The office renderer fetches the document itself, so the dispatcher must not
// open the blob for it — on an S3 backend that is a full file download thrown
// straight away.
func TestOfficeRendererDeclaresItFetchesOwnSource(t *testing.T) {
	r := &OfficeRenderer{}
	sf, ok := any(r).(selfFetching)
	if !ok {
		t.Fatal("OfficeRenderer must implement selfFetching")
	}
	if !sf.FetchesOwnSource() {
		t.Error("FetchesOwnSource should be true")
	}
	// The renderers that do read bytes must not claim otherwise.
	for name, other := range map[string]Renderer{"image": ImageRenderer{}, "text": TextRenderer{}} {
		if s, ok := any(other).(selfFetching); ok && s.FetchesOwnSource() {
			t.Errorf("%s renderer must not skip the blob it reads", name)
		}
	}
}
