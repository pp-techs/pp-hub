package preview

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

// minimalDocx builds the smallest .docx OnlyOffice will accept, so the test
// needs no binary fixture checked into the repo.
func minimalDocx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>pp-hub office thumbnail test</w:t></w:r></w:p></w:body>
</w:document>`,
	}
	for name, body := range parts {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestOfficeAgainstRealOnlyOffice drives the actual conversion API.
//
// The mocked tests above pin the request and response shapes; this one proves
// those shapes are the ones OnlyOffice really uses. It needs a running
// Document Server, and a hostname that server can reach *this test* on —
// OnlyOffice fetches the document itself, so 127.0.0.1 is no good from inside
// its container:
//
//	PP_TEST_ONLYOFFICE_URL=http://localhost:9980 \
//	PP_TEST_CALLBACK_HOST=host.docker.internal \
//	PP_TEST_ONLYOFFICE_JWT_SECRET=<the Document Server's JWT_SECRET> \
//	go test ./internal/preview/ -run RealOnlyOffice
//
// The secret is needed whenever the Document Server runs with JWT_ENABLED=true,
// which both compose stacks do.
func TestOfficeAgainstRealOnlyOffice(t *testing.T) {
	ooURL := os.Getenv("PP_TEST_ONLYOFFICE_URL")
	callbackHost := os.Getenv("PP_TEST_CALLBACK_HOST")
	if ooURL == "" || callbackHost == "" {
		t.Skip("PP_TEST_ONLYOFFICE_URL / PP_TEST_CALLBACK_HOST not set; skipping live conversion")
	}

	docx := minimalDocx(t)

	// Bind to all interfaces: the default httptest listener is loopback-only,
	// which the OnlyOffice container cannot reach.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		_, _ = w.Write(docx)
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	srcURL := fmt.Sprintf("http://%s:%d/test.docx", callbackHost, port)

	r := &officeConverter{
		cfg: OfficeConfig{ConvertURL: ooURL, JWTSecret: []byte(os.Getenv("PP_TEST_ONLYOFFICE_JWT_SECRET"))},
		cli: &http.Client{Timeout: convertTimeout},
	}
	payload := Payload{Name: "test.docx", BlobHash: "pphublivetest0001"}

	pdfURL, err := r.convert(t.Context(), srcURL, payload)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	pdfBytes, err := r.download(t.Context(), pdfURL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("converted output is not a PDF (first bytes: %q)", pdfBytes[:min(8, len(pdfBytes))])
	}

	// Rasterize and read the text too, when poppler is present — that is the
	// rest of both chains.
	if pdf := NewPDFRenderer(); pdf != nil {
		thumb, err := pdf.Render(t.Context(), bytes.NewReader(pdfBytes), payload)
		if err != nil {
			t.Fatalf("rasterize converted pdf: %v", err)
		}
		decodeThumb(t, thumb)
	} else {
		t.Logf("converted %d bytes of PDF; skipping rasterization (%s not installed)", len(pdfBytes), pdfBinary)
	}
	if hasBinary(pdfTextBinary) {
		text, err := pdfText(t.Context(), bytes.NewReader(pdfBytes))
		if err != nil {
			t.Fatalf("read converted pdf text: %v", err)
		}
		if !strings.Contains(text, "pp-hub office thumbnail test") {
			t.Errorf("converted pdf text = %q, want the document's sentence", text)
		}
	} else {
		t.Logf("skipping text extraction (%s not installed)", pdfTextBinary)
	}
}
