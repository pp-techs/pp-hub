package preview

import (
	"bytes"
	"context"
	"io"
)

// OfficeTextExtractor reads the text of a word-processing document,
// spreadsheet or presentation: OnlyOffice converts it to PDF — the same
// conversion, under the same cache key, the thumbnail renderer asks for — and
// pdftotext reads that.
//
// A PDF is what is read rather than OnlyOffice's own text output because one
// path then serves every format, spreadsheets and slides included, and the
// extractor inherits the converter's handling of legacy binary formats
// (.doc, .xls, .ppt) instead of reimplementing it. The cost is that a
// spreadsheet is read as it would print: cells outside its print area or on
// hidden sheets are not indexed.
type OfficeTextExtractor struct {
	conv *officeConverter
}

// NewOfficeTextExtractor returns an extractor, or nil when it cannot work:
// the same OnlyOffice and signing-key requirements as NewOfficeRenderer, plus
// pdftotext for the converted PDF. A nil extractor leaves extract:office jobs
// pending for a worker that has all of them.
func NewOfficeTextExtractor(cfg OfficeConfig) Extractor {
	if !cfg.usable() || !hasBinary(pdfTextBinary) {
		return nil
	}
	return &OfficeTextExtractor{conv: newOfficeConverter(cfg)}
}

func (*OfficeTextExtractor) Kind() string { return KindExtractOffice }

// FetchesOwnSource keeps the dispatcher from opening the blob: OnlyOffice
// downloads the document itself.
func (*OfficeTextExtractor) FetchesOwnSource() bool { return true }

// Extract ignores src, for the reason FetchesOwnSource gives.
func (e *OfficeTextExtractor) Extract(ctx context.Context, _ io.Reader, p Payload) (Extraction, error) {
	pdf, err := e.conv.toPDF(ctx, p)
	if err != nil {
		return Extraction{}, err
	}
	text, err := pdfText(ctx, bytes.NewReader(pdf))
	return Extraction{Text: text}, err
}
