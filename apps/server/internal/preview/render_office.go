package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/wopi"
)

const (
	// convertTimeout bounds the whole round trip: OnlyOffice fetching the
	// document, converting it, and us downloading the result.
	convertTimeout = 60 * time.Second
	// sourceTokenTTL is how long the URL we hand OnlyOffice stays valid.
	// The fetch happens within seconds; nothing here waits on a human.
	sourceTokenTTL = 5 * time.Minute
	// maxConvertedPDF caps the intermediate document we download back.
	maxConvertedPDF = 64 << 20
)

// OfficeConfig wires the renderer to the two services it needs.
type OfficeConfig struct {
	// ConvertURL is OnlyOffice as reachable from this process — the container
	// network address, not the browser-facing one.
	ConvertURL string
	// SourceBaseURL is this deployment's API server as reachable from the
	// OnlyOffice container (PP_WOPI_SERVER_URL).
	SourceBaseURL string
	// WopiKey signs the short-lived token on the source URL.
	WopiKey []byte
	// JWTSecret is the Document Server's shared signing secret
	// (PP_ONLYOFFICE_JWT_SECRET, matching its JWT_SECRET). Empty means the
	// Document Server is running with JWT_ENABLED=false and will accept
	// requests from anything that can reach it.
	JWTSecret []byte
}

// OfficeRenderer thumbnails office documents by converting them to PDF with
// OnlyOffice and rasterizing the first page.
//
// It is the only renderer that depends on other services being up, and the
// reason is structural: OnlyOffice's conversion API fetches the document by
// URL rather than accepting an upload, and cmd/worker has no listener to serve
// one from. So the worker mints a short-lived read-only WOPI token and points
// OnlyOffice at the API server's existing /wopi/files/{id}/contents route.
//
//	worker ──POST /converter {url: api/wopi/…?access_token=…}──> OnlyOffice
//	                                                                  │
//	worker <────────────── {fileUrl: …pdf} ───────────────────────────┘
//	   └── GET fileUrl → PDF bytes → pdftoppm → imageThumbnail
type OfficeRenderer struct {
	cfg OfficeConfig
	pdf Renderer
	cli *http.Client
}

// NewOfficeRenderer returns a renderer, or nil when it cannot work: office
// conversion needs OnlyOffice reachable, the API server's address for the
// callback, a session key to sign with, and poppler to rasterize the result.
// Returning nil leaves render:office jobs pending for a worker that has all
// four, rather than failing them here.
func NewOfficeRenderer(cfg OfficeConfig) Renderer {
	if cfg.ConvertURL == "" || cfg.SourceBaseURL == "" || len(cfg.WopiKey) == 0 {
		return nil
	}
	pdf := NewPDFRenderer()
	if pdf == nil {
		return nil
	}
	return &OfficeRenderer{
		cfg: cfg,
		pdf: pdf,
		cli: &http.Client{Timeout: convertTimeout},
	}
}

func (r *OfficeRenderer) Kind() string { return KindRenderOffice }

// FetchesOwnSource tells the dispatcher not to open the blob: OnlyOffice
// downloads the document itself from the URL we hand it, so reading the bytes
// here would fetch the whole file only to throw it away.
func (r *OfficeRenderer) FetchesOwnSource() bool { return true }

// Render ignores src — OnlyOffice fetches the bytes itself, and
// FetchesOwnSource stops the dispatcher from opening the blob at all.
func (r *OfficeRenderer) Render(ctx context.Context, _ io.Reader, p Payload) ([]byte, error) {
	if p.NodeID == "" {
		return nil, fmt.Errorf("office: job payload has no node id")
	}
	srcURL, err := r.sourceURL(p.NodeID)
	if err != nil {
		return nil, err
	}
	pdfURL, err := r.convert(ctx, srcURL, p)
	if err != nil {
		return nil, err
	}
	pdfBytes, err := r.download(ctx, pdfURL)
	if err != nil {
		return nil, err
	}
	return r.pdf.Render(ctx, bytes.NewReader(pdfBytes), p)
}

// sourceURL builds the tokenized URL OnlyOffice fetches the document from.
func (r *OfficeRenderer) sourceURL(nodeID string) (string, error) {
	var nid pgtype.UUID
	if err := nid.Scan(nodeID); err != nil {
		return "", fmt.Errorf("office: bad node id %q: %w", nodeID, err)
	}
	// Rank 1 is view — the lowest that opens the file. A conversion never
	// writes, so handing out anything more would be a gift to whoever gets
	// hold of the URL in the next five minutes.
	token, err := wopi.SignTokenWithTTL(r.cfg.WopiKey, pgtype.UUID{}, nid, 1, sourceTokenTTL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/wopi/files/%s/contents?access_token=%s",
		strings.TrimRight(r.cfg.SourceBaseURL, "/"), nodeID, url.QueryEscape(token)), nil
}

// convertRequest is the OnlyOffice conversion API payload.
// https://api.onlyoffice.com/docs/docs-api/additional-api/conversion-api/request/
type convertRequest struct {
	Async      bool   `json:"async"`
	URL        string `json:"url"`
	FileType   string `json:"filetype"`
	OutputType string `json:"outputtype"`
	Key        string `json:"key"`
	Title      string `json:"title"`
	// Token is the request above, signed. Omitted when the Document Server
	// runs unsigned — sending one it cannot verify is worse than sending
	// none, because it fails with the same opaque code as a wrong secret.
	Token string `json:"token,omitempty"`
}

type convertResponse struct {
	EndConvert bool   `json:"endConvert"`
	FileURL    string `json:"fileUrl"`
	Percent    int    `json:"percent"`
	Error      int    `json:"error"`
}

func (r *OfficeRenderer) convert(ctx context.Context, srcURL string, p Payload) (string, error) {
	payload := convertRequest{
		// Synchronous: the worker already runs off the request path, so
		// polling an async job would add a state machine for nothing.
		Async:      false,
		URL:        srcURL,
		FileType:   strings.TrimPrefix(strings.ToLower(path.Ext(p.Name)), "."),
		OutputType: "pdf",
		// The blob hash is a perfect cache key — content-addressed, so the
		// same document converted twice is a cache hit inside OnlyOffice.
		Key:   convertKey(p.BlobHash),
		Title: p.Name,
	}

	// The signature covers the request, so the URL the Document Server is
	// told to fetch cannot be swapped by anyone who intercepts the call —
	// and, more to the point, nobody who merely reaches its port can ask it
	// to fetch anything at all.
	var bearer string
	if len(r.cfg.JWTSecret) > 0 {
		signed, err := signOnlyOfficeJWT(r.cfg.JWTSecret, payload)
		if err != nil {
			return "", fmt.Errorf("office: sign convert request: %w", err)
		}
		payload.Token = signed
		// The header form wraps the same request under `payload`; the
		// Document Server checks whichever it is configured for.
		bearer, err = signOnlyOfficeJWT(r.cfg.JWTSecret, map[string]any{"payload": payload})
		if err != nil {
			return "", fmt.Errorf("office: sign convert header: %w", err)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(r.cfg.ConvertURL, "/")+"/converter", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Without this the service answers in XML.
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := r.cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("office: convert request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("office: convert returned %s", resp.Status)
	}

	var out convertResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("office: decode convert response: %w", err)
	}
	if out.Error != 0 {
		return "", fmt.Errorf("office: convert failed: %s", convertError(out.Error))
	}
	if !out.EndConvert || out.FileURL == "" {
		return "", fmt.Errorf("office: convert incomplete (%d%%)", out.Percent)
	}
	return out.FileURL, nil
}

func (r *OfficeRenderer) download(ctx context.Context, fileURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("office: fetch converted pdf: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("office: fetch converted pdf returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxConvertedPDF))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("office: converted pdf was empty")
	}
	return data, nil
}

// convertKey builds OnlyOffice's document cache key. It must be at most 128
// chars and alphanumeric-ish; a blake3 hex hash already is.
func convertKey(blobHash string) string {
	if len(blobHash) > 64 {
		return blobHash[:64]
	}
	return blobHash
}

// convertError translates OnlyOffice's numeric error codes, which are
// otherwise opaque in jobs.last_error.
// https://api.onlyoffice.com/docs/docs-api/additional-api/conversion-api/response/
func convertError(code int) string {
	switch code {
	case -1:
		return "-1 unknown conversion error"
	case -2:
		return "-2 conversion timeout"
	case -3:
		return "-3 conversion error (document may be corrupt)"
	case -4:
		return "-4 could not download the source — is PP_WOPI_SERVER_URL reachable from the OnlyOffice container?"
	case -5:
		return "-5 incorrect password"
	case -6:
		return "-6 error accessing the document database"
	case -7:
		return "-7 input error"
	case -8:
		return "-8 invalid token — OnlyOffice has JWT enabled but this request was unsigned"
	default:
		return fmt.Sprintf("%d", code)
	}
}
