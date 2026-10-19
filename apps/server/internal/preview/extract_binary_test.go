package preview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// Every type with a thumbnail from OnlyOffice is indexed through it, PDFs
// through poppler, and nothing else binary is.
func TestExtractKindForMime(t *testing.T) {
	cases := map[string]string{
		"text/markdown":                           KindExtractText,
		"text/plain; charset=utf-8":               KindExtractText,
		"application/pdf":                         KindExtractPDF,
		"Application/PDF":                         KindExtractPDF,
		"application/msword":                      KindExtractOffice,
		"application/vnd.ms-excel":                KindExtractOffice,
		"application/vnd.ms-powerpoint":           KindExtractOffice,
		"application/octet-stream":                "",
		"application/epub+zip":                    "",
		"image/png":                               "",
		"text/csv":                                "",
		"application/rtf":                         "",
		"application/vnd.oasis.opendocument.text": KindExtractOffice,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": KindExtractOffice,
	}
	for mime, want := range cases {
		if got := extractKindForMime(mime); got != want {
			t.Errorf("extractKindForMime(%q) = %q, want %q", mime, got, want)
		}
	}
	for mime := range officeMimes {
		if extractKindForMime(mime) != KindExtractOffice {
			t.Errorf("%q has a thumbnail but is not indexed", mime)
		}
	}
}

// runText keeps what a converter printed, however much, and tells a document
// with no text from a converter that failed.
func TestRunText(t *testing.T) {
	ctx := context.Background()

	got, err := runText(ctx, "sh", "-c", "printf 'hello  wörld'")
	if err != nil || got != "hello  wörld" {
		t.Errorf("runText = %q, %v", got, err)
	}

	// No text is a result, not a failure: a scanned PDF is still indexed.
	if got, err := runText(ctx, "true"); err != nil || got != "" {
		t.Errorf("empty output: %q, %v", got, err)
	}

	if _, err := runText(ctx, "sh", "-c", "echo broken >&2; exit 3"); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("a failing converter: %v", err)
	}

	// Output without end stops at the cap and succeeds: the index would drop
	// the tail anyway, so the child is stopped rather than read to the end.
	got, err = runText(ctx, "yes", "word")
	if err != nil || len(got) != maxExtractedText {
		t.Errorf("unbounded output: len=%d, %v; want %d, nil", len(got), err, maxExtractedText)
	}
}

// textPDF is a one-page PDF whose text layer is exactly lines, in order.
func textPDF(lines ...string) []byte {
	var stream strings.Builder
	stream.WriteString("BT /F1 12 Tf 14 TL 72 720 Td\n")
	for _, l := range lines {
		fmt.Fprintf(&stream, "(%s) Tj T*\n", strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(l))
	}
	stream.WriteString("ET")

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", stream.Len(), stream.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return out.Bytes()
}

func TestPDFTextExtractor(t *testing.T) {
	ex := NewPDFTextExtractor()
	if ex == nil {
		t.Skip("pdftotext not installed")
	}
	ctx := context.Background()

	got, err := ex.Extract(ctx, bytes.NewReader(textPDF("Quarterly budget", "forecast (draft)")), Payload{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, want := range []string{"Quarterly budget", "forecast (draft)"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("text %q lacks %q", got.Text, want)
		}
	}
	if got.Page != nil {
		t.Error("a PDF is not a block document and has no page index")
	}

	// A page with no text layer is indexed as empty, not failed.
	got, err = ex.Extract(ctx, bytes.NewReader(textPDF()), Payload{})
	if err != nil || strings.TrimSpace(got.Text) != "" {
		t.Errorf("empty page: %q, %v", got.Text, err)
	}

	if _, err := ex.Extract(ctx, strings.NewReader("this is not a pdf"), Payload{}); err == nil {
		t.Error("a corrupt PDF must fail, not index as empty")
	}
}

// The office extractor must decline rather than fail jobs when it cannot
// work, exactly as the renderer does.
func TestNewOfficeTextExtractorDeclinesWhenUnconfigured(t *testing.T) {
	full := OfficeConfig{ConvertURL: "http://onlyoffice", SourceBaseURL: "http://server:8080", WopiKey: testKey}
	for name, cfg := range map[string]OfficeConfig{
		"no convert url": {SourceBaseURL: full.SourceBaseURL, WopiKey: full.WopiKey},
		"no source url":  {ConvertURL: full.ConvertURL, WopiKey: full.WopiKey},
		"no session key": {ConvertURL: full.ConvertURL, SourceBaseURL: full.SourceBaseURL},
	} {
		if NewOfficeTextExtractor(cfg) != nil {
			t.Errorf("%s: expected nil extractor", name)
		}
	}
	got := NewOfficeTextExtractor(full)
	if hasBinary(pdfTextBinary) != (got != nil) {
		t.Errorf("pdftotext present=%v but extractor=%v", hasBinary(pdfTextBinary), got != nil)
	}
}

// recordingExtractor stands in for the extractors that need poppler and
// OnlyOffice, recording what the dispatcher handed it.
type recordingExtractor struct {
	kind string
	self bool
	text string

	src     string
	payload Payload
}

func (r *recordingExtractor) Kind() string           { return r.kind }
func (r *recordingExtractor) FetchesOwnSource() bool { return r.self }
func (r *recordingExtractor) Extract(_ context.Context, src io.Reader, p Payload) (Extraction, error) {
	b, _ := io.ReadAll(src)
	r.src, r.payload = string(b), p
	return Extraction{Text: r.text}, nil
}

// A PDF and an office document are indexed by the kind their type maps to,
// the office extractor is not handed the bytes it fetches itself, and files
// stored before either was indexed are queued once by the backfill.
func TestBinaryDocumentsAreExtractedByTheirOwnKinds(t *testing.T) {
	e := newExtractEnv(t)
	ctx := context.Background()
	pdf := &recordingExtractor{kind: KindExtractPDF, text: "Báo cáo quý ba"}
	office := &recordingExtractor{kind: KindExtractOffice, self: true, text: "Bảng lương tháng mười"}
	e.w.WithExtractors(pdf, office)

	pdfNode, _ := e.put(t, pgtype.UUID{}, "report.pdf", "%PDF-fake")
	docNode, docVer := e.put(t, pgtype.UUID{}, "payroll.docx", "PK-fake")
	e.drainAll(t)

	if _, content, ok := e.indexed(t, pdfNode.ID); !ok || content != pdf.text {
		t.Errorf("pdf indexed as %q (ok=%v)", content, ok)
	}
	if pdf.src != "%PDF-fake" {
		t.Errorf("pdf extractor read %q, want the stored bytes", pdf.src)
	}
	if _, content, ok := e.indexed(t, docNode.ID); !ok || content != office.text {
		t.Errorf("office document indexed as %q (ok=%v)", content, ok)
	}
	if office.src != "" {
		t.Errorf("the dispatcher opened the blob for a self-fetching extractor: %q", office.src)
	}
	if office.payload.NodeID != uuidStr(docNode.ID) || office.payload.Name != "payroll.docx" {
		t.Errorf("office payload = %+v", office.payload)
	}

	// Restoring re-indexes from the version row alone: no name, but the MIME
	// type still says which parser OnlyOffice needs.
	if err := e.w.Reindex(ctx, docVer); err != nil {
		t.Fatal(err)
	}
	e.drainAll(t)
	if office.payload.Name != "" || officeFileType(office.payload) != "docx" {
		t.Errorf("reindex payload = %+v, file type %q", office.payload, officeFileType(office.payload))
	}

	// The backfill queues one extraction of the right kind per old file.
	e.fs.SetVersionHook(nil)
	oldPDF, oldVer := e.put(t, pgtype.UUID{}, "legacy.pdf", "%PDF-old")
	for range 2 {
		if _, err := BackfillText(ctx, e.q); err != nil {
			t.Fatalf("backfill: %v", err)
		}
	}
	var n int
	if err := e.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE kind = $1 AND payload->>'node_version_id' = $2`,
		KindExtractPDF, oldVer.ID.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("backfill queued %d extract:pdf jobs for the version, want 1", n)
	}
	e.drainAll(t)
	if _, content, ok := e.indexed(t, oldPDF.ID); !ok || content != pdf.text {
		t.Errorf("backfilled pdf indexed as %q (ok=%v)", content, ok)
	}
}

// Without an extractor for a kind the jobs wait rather than fail, so a
// worker deployed later drains them.
func TestBinaryExtractionWaitsForAWorker(t *testing.T) {
	e := newExtractEnv(t)
	ctx := context.Background()
	node, ver := e.put(t, pgtype.UUID{}, "waiting.pdf", "%PDF-fake")
	e.w.drain(ctx)

	var status string
	if err := e.pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE kind = $1 AND payload->>'node_version_id' = $2`,
		KindExtractPDF, ver.ID.String()).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Errorf("job is %q, want pending", status)
	}
	if _, _, ok := e.indexed(t, node.ID); ok {
		t.Error("indexed without an extractor")
	}
	// Leave nothing pending for the next test's drain.
	if _, err := e.pool.Exec(ctx, `DELETE FROM jobs WHERE kind = $1 AND payload->>'node_version_id' = $2`,
		KindExtractPDF, ver.ID.String()); err != nil {
		t.Fatal(err)
	}
}
