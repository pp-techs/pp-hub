package preview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

var testKey = []byte("pphub-test-session-key-32bytes!!")

// The renderer must decline rather than fail jobs when it cannot possibly
// work. Each missing piece leaves render:office pending for a worker that has
// everything, instead of burning five attempts and landing in 'failed'.
func TestNewOfficeRendererDeclinesWhenUnconfigured(t *testing.T) {
	full := OfficeConfig{
		ConvertURL:    "http://onlyoffice",
		SourceBaseURL: "http://server:8080",
		WopiKey:       testKey,
	}
	cases := map[string]OfficeConfig{
		"no convert url": {SourceBaseURL: full.SourceBaseURL, WopiKey: full.WopiKey},
		"no source url":  {ConvertURL: full.ConvertURL, WopiKey: full.WopiKey},
		"no session key": {ConvertURL: full.ConvertURL, SourceBaseURL: full.SourceBaseURL},
		"all missing":    {},
	}
	for name, cfg := range cases {
		if r := NewOfficeRenderer(cfg); r != nil {
			t.Errorf("%s: expected nil renderer, got one", name)
		}
	}

	// With everything present it still depends on poppler, since it rasterizes
	// the converted PDF through the same path as a native PDF.
	got := NewOfficeRenderer(full)
	if hasBinary(pdfBinary) && got == nil {
		t.Error("fully configured with poppler present: expected a renderer")
	}
	if !hasBinary(pdfBinary) && got != nil {
		t.Error("poppler missing: expected nil, since the converted PDF could not be rasterized")
	}
}

// The URL handed to OnlyOffice must carry a token the API server will accept,
// and must not grant more than reading.
func TestOfficeSourceURLIsSignedAndReadOnly(t *testing.T) {
	r := &OfficeRenderer{cfg: OfficeConfig{
		SourceBaseURL: "http://server:8080/",
		WopiKey:       testKey,
	}}
	nodeID := "11111111-2222-3333-4444-555555555555"
	got, err := r.sourceURL(nodeID)
	if err != nil {
		t.Fatalf("sourceURL: %v", err)
	}
	if !strings.HasPrefix(got, "http://server:8080/wopi/files/"+nodeID+"/contents?access_token=") {
		t.Fatalf("unexpected URL shape: %s", got)
	}

	// Extract and verify the token exactly as the API server would.
	raw := got[strings.Index(got, "access_token=")+len("access_token="):]
	token, err := url.QueryUnescape(raw)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := wopi.NewService(nil, testKey).VerifyToken(token)
	if err != nil {
		t.Fatalf("server would reject the minted token: %v", err)
	}
	if claims.PermRank != 1 {
		t.Errorf("PermRank = %d, want 1 (view only)", claims.PermRank)
	}
	gotNode, _ := claims.NodeID.Value()
	if gotNode != nodeID {
		t.Errorf("token is for node %v, want %s", gotNode, nodeID)
	}
	// Signed for no user: nothing here acts on someone's behalf.
	gotUser, _ := claims.UserID.Value()
	if gotUser != wopi.SystemSubject {
		t.Errorf("subject = %v, want the system subject", gotUser)
	}
}

// A token signed with a different key must be rejected — this is the failure
// an operator hits when the worker and server disagree on PP_SESSION_KEY, and
// it is why the worker refuses to register the renderer in that case.
func TestOfficeTokenFailsUnderAKeyMismatch(t *testing.T) {
	r := &OfficeRenderer{cfg: OfficeConfig{
		SourceBaseURL: "http://server:8080",
		WopiKey:       testKey,
	}}
	got, err := r.sourceURL("11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	raw := got[strings.Index(got, "access_token=")+len("access_token="):]
	token, _ := url.QueryUnescape(raw)

	other := wopi.NewService(nil, []byte("a-completely-different-32b-key!!!"))
	if _, err := other.VerifyToken(token); err == nil {
		t.Error("a token signed with another key must not verify")
	}
}

func TestOfficeRenderRejectsPayloadWithoutNodeID(t *testing.T) {
	r := &OfficeRenderer{cfg: OfficeConfig{SourceBaseURL: "http://s", WopiKey: testKey}}
	if _, err := r.Render(t.Context(), strings.NewReader(""), Payload{}); err == nil {
		t.Error("expected an error when the payload carries no node id")
	}
}

// OnlyOffice reports failures in the body with HTTP 200, so a naive status
// check would treat a failed conversion as success and hand an empty URL on.
func TestOfficeConvertSurfacesBodyErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(convertResponse{Error: -4})
	}))
	defer srv.Close()

	r := &OfficeRenderer{cfg: OfficeConfig{ConvertURL: srv.URL}, cli: srv.Client()}
	_, err := r.convert(t.Context(), "http://src", Payload{Name: "a.docx"})
	if err == nil {
		t.Fatal("expected an error for error code -4")
	}
	// The numeric code alone is unreadable in jobs.last_error.
	if !strings.Contains(err.Error(), "PP_WOPI_SERVER_URL") {
		t.Errorf("error should explain code -4, got: %v", err)
	}
}

func TestOfficeConvertRequestShape(t *testing.T) {
	var got convertRequest
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(convertResponse{EndConvert: true, FileURL: "http://oo/out.pdf"})
	}))
	defer srv.Close()

	r := &OfficeRenderer{cfg: OfficeConfig{ConvertURL: srv.URL}, cli: srv.Client()}
	url, err := r.convert(t.Context(), "http://src", Payload{Name: "Q3 Report.docx", BlobHash: "abc123"})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if url != "http://oo/out.pdf" {
		t.Errorf("fileUrl = %q", url)
	}
	// Without this header the service answers in XML and decoding fails.
	if accept != "application/json" {
		t.Errorf("Accept = %q, want application/json", accept)
	}
	if got.FileType != "docx" {
		t.Errorf("filetype = %q, want docx (from the name's extension)", got.FileType)
	}
	if got.OutputType != "pdf" {
		t.Errorf("outputtype = %q, want pdf", got.OutputType)
	}
	if got.Async {
		t.Error("async must be false; the worker is already off the request path")
	}
	// Content-addressed key means the same document converts once, ever.
	if got.Key != "abc123" {
		t.Errorf("key = %q, want the blob hash", got.Key)
	}
}

// A response claiming success but carrying no URL must not be treated as one.
func TestOfficeConvertRejectsIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(convertResponse{EndConvert: false, Percent: 40})
	}))
	defer srv.Close()

	r := &OfficeRenderer{cfg: OfficeConfig{ConvertURL: srv.URL}, cli: srv.Client()}
	if _, err := r.convert(t.Context(), "http://src", Payload{Name: "a.docx"}); err == nil {
		t.Error("expected an error when endConvert is false")
	}
}

func TestOfficeMimeMapping(t *testing.T) {
	office := []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/msword",
		"application/vnd.oasis.opendocument.text",
	}
	for _, m := range office {
		if got := kindForMime(m); got != KindRenderOffice {
			t.Errorf("kindForMime(%q) = %q, want %q", m, got, KindRenderOffice)
		}
		if got := previewKindFor(KindRenderOffice); got != PreviewThumbnail {
			t.Errorf("office should produce a thumbnail, got %q", got)
		}
	}
	// Types the editor opens but that must not get a speculative render.
	for _, m := range []string{"application/octet-stream", "application/epub+zip"} {
		if got := kindForMime(m); got == KindRenderOffice {
			t.Errorf("kindForMime(%q) should not be render:office", m)
		}
	}
	// .csv and .rtf keep their text snippet rather than becoming documents.
	for _, m := range []string{"text/csv", "application/rtf"} {
		if got := kindForMime(m); got != KindSnippetText {
			t.Errorf("kindForMime(%q) = %q, want %q", m, got, KindSnippetText)
		}
	}
}

func TestSignTokenWithTTLNeedsANode(t *testing.T) {
	if _, err := wopi.SignTokenWithTTL(testKey, pgtype.UUID{}, pgtype.UUID{}, 1, sourceTokenTTL); err == nil {
		t.Error("expected an error when no node id is given")
	}
}
