package preview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Job kinds of the extractors that need cmd/worker — they run poppler, and
// for office documents also OnlyOffice, so like a render kind they stay
// pending until a process that registered an Extractor for them claims them.
const (
	KindExtractPDF    = "extract:pdf"
	KindExtractOffice = "extract:office"
)

const (
	// pdfTextBinary is poppler's text extractor, shipped beside pdftoppm in
	// the poppler-utils package.
	pdfTextBinary = "pdftotext"
	// extractTimeout bounds one text extraction. It is longer than a render's
	// because a long document is slow to walk rather than hostile, and
	// reading stops at maxExtractedText long before the end of a long one.
	extractTimeout = 60 * time.Second
	// maxExtractedText is how much of a converter's output is read. The index
	// keeps textsearch.MaxIndexedBytes after normalising; this leaves room
	// for the whitespace and invalid bytes that normalising drops.
	maxExtractedText = 1 << 20
)

// PDFTextExtractor reads the text layer of a PDF with pdftotext.
//
// Like pdftoppm it parses attacker-supplied documents in a memory-unsafe C
// stack, so it runs in cmd/worker's container. A PDF with no text layer — a
// scan — yields an empty text, which is stored: the file is indexed, it just
// has nothing to match. OCR is not attempted.
type PDFTextExtractor struct{}

// NewPDFTextExtractor returns an extractor, or nil when poppler is not
// installed, so the process never claims extract:pdf jobs it cannot run.
func NewPDFTextExtractor() Extractor {
	if !hasBinary(pdfTextBinary) {
		return nil
	}
	return PDFTextExtractor{}
}

func (PDFTextExtractor) Kind() string { return KindExtractPDF }

func (PDFTextExtractor) Extract(ctx context.Context, src io.Reader, _ Payload) (Extraction, error) {
	text, err := pdfText(ctx, src)
	return Extraction{Text: text}, err
}

// pdfText spools a PDF — pdftotext reads the trailer to find the page tree —
// and returns its text in reading order, page breaks dropped.
func pdfText(ctx context.Context, src io.Reader) (string, error) {
	path, cleanup, err := spool(src, "in.pdf")
	if err != nil {
		return "", err
	}
	defer cleanup()
	return runText(ctx, pdfTextBinary, "-enc", "UTF-8", "-nopgbrk", path, "-")
}

// runText executes a binary that prints text on stdout and returns what it
// printed, up to maxExtractedText bytes.
//
// Unlike runCapture, a long output is not an error: the index would drop the
// tail anyway, so once the cap is reached the child is stopped and what was
// read stands. An empty output is not an error either — a document can have
// no text. A deadline that expires before the cap or the end of the output is
// one, because the text read so far is an arbitrary prefix of an unfinished
// walk.
func runText(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}

	out, readErr := io.ReadAll(io.LimitReader(stdout, maxExtractedText))
	full := len(out) >= maxExtractedText
	if full {
		cancel()
	}
	waitErr := cmd.Wait()

	switch {
	case full:
		return string(out), nil
	case ctx.Err() != nil:
		return "", fmt.Errorf("%s timed out after %s", name, extractTimeout)
	case readErr != nil:
		return "", fmt.Errorf("%s: read output: %w", name, readErr)
	case waitErr != nil:
		return "", fmt.Errorf("%s: %w: %s", name, waitErr, firstLine(stderr.String()))
	}
	return string(out), nil
}
