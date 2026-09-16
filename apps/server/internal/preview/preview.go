// Package preview implements background job workers that generate file
// previews (image/PDF/video thumbnails, text snippets).
//
// Two different kinds live here and must not be confused:
//
//   - a *job* kind ("render:pdf") is a routing key — it says which process is
//     able to claim the work, and only workers that registered a matching
//     Renderer ever will.
//   - a *preview* kind ("thumbnail") is the artifact — it is what the browser
//     asks for at GET /nodes/{id}/preview?kind=…
//
// They used to be the same string, which forced this file and
// apps/web/src/lib/api/utils.ts to agree exactly. They no longer do: PDF,
// video and office all render to a thumbnail, and how one was produced is
// nobody's business but this package's.
//
// See docs/architecture/preview-pipeline.md.
package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

func init() {
	// Register MIME types that Go's stdlib doesn't know by default.
	mime.AddExtensionType(".epub", "application/epub+zip")
	mime.AddExtensionType(".rtf", "application/rtf")
}

// Job kinds — the routing keys workers claim by.
const (
	KindRenderImage  = "render:image"
	KindRenderPDF    = "render:pdf"
	KindRenderVideo  = "render:video"
	KindRenderOffice = "render:office"
	KindSnippetText  = "snippet:text"
)

// Preview kinds — the artifacts clients request.
const (
	PreviewThumbnail = "thumbnail"
	PreviewText      = "text"
)

const maxAttempts = 5

// jobLease is how long a claimed job may stay 'running' before another worker
// may take it back. Generously longer than any render can legitimately take —
// renderTimeout bounds the external binaries at 30s, and the rest is spooling
// bytes — so reclaiming can never race a worker that is still busy.
const jobLease = 10 * time.Minute

// Renderer turns a file's bytes into a preview blob. A process advertises
// exactly the renderers it registered, so a binary can never claim work it
// has no way of doing.
type Renderer interface {
	// Kind is the job kind this renderer handles, e.g. KindRenderPDF.
	Kind() string
	// Render reads the original file and returns the preview blob.
	Render(ctx context.Context, src io.Reader, p Payload) ([]byte, error)
}

// selfFetching is implemented by a Renderer that reads the original file
// itself. The dispatcher then skips opening the blob for it.
//
// Only the office renderer does this, and it matters: OnlyOffice fetches the
// document by URL, so opening the blob here would pull the whole file —
// across the network, on an S3 backend — purely to discard it.
type selfFetching interface {
	FetchesOwnSource() bool
}

// Worker claims jobs and generates previews.
type Worker struct {
	q         *store.Queries
	blobs     blob.Store
	renderers map[string]Renderer
	kinds     []string // derived from renderers; the ClaimJobs filter
}

// NewWorker registers the renderers this process can run. A Worker with no
// renderers never claims anything — it is still useful for GetPreviewBlob,
// which is how the API server reads previews it did not generate.
func NewWorker(q *store.Queries, blobs blob.Store, rs ...Renderer) *Worker {
	w := &Worker{q: q, blobs: blobs, renderers: make(map[string]Renderer, len(rs))}
	for _, r := range rs {
		w.renderers[r.Kind()] = r
	}
	w.kinds = slices.Sorted(maps.Keys(w.renderers))
	return w
}

// Kinds are the job kinds this worker will claim.
func (w *Worker) Kinds() []string { return w.kinds }

// Payload for preview jobs.
type Payload struct {
	NodeVersionID string `json:"node_version_id"`
	NodeID        string `json:"node_id"`
	BlobHash      string `json:"blob_hash"`
	Mime          string `json:"mime"`
	Name          string `json:"name"`
}

// Enqueue schedules a preview job for a new version.
func (w *Worker) Enqueue(ctx context.Context, ver store.NodeVersion, name string) error {
	kind := kindForMime(ver.Mime)
	if kind == "" {
		return nil // no preview for this type
	}
	payload, _ := json.Marshal(Payload{
		NodeVersionID: uuidStr(ver.ID),
		NodeID:        uuidStr(ver.NodeID),
		BlobHash:      ver.BlobHash,
		Mime:          ver.Mime,
		Name:          name,
	})
	_, err := w.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: kind, Payload: payload})
	return err
}

// officeMimes render through OnlyOffice's conversion API.
//
// Deliberately narrower than httpapi.isOfficeMime, which decides what the
// *editor* opens: that list includes application/octet-stream as a fallback
// and text/csv, neither of which should have a document thumbnail rendered
// speculatively. .epub and .rtf are also absent — .rtf already gets a text
// snippet, and .epub is a zip the editor handles but a cover render would
// misrepresent.
var officeMimes = map[string]bool{
	// Word processing
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true, // .docx
	"application/msword":                      true, // .doc
	"application/vnd.oasis.opendocument.text": true, // .odt
	// Spreadsheets
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true, // .xlsx
	"application/vnd.ms-excel":                       true, // .xls
	"application/vnd.oasis.opendocument.spreadsheet": true, // .ods
	// Presentations
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true, // .pptx
	"application/vnd.ms-powerpoint":                                             true, // .ppt
	"application/vnd.oasis.opendocument.presentation":                           true, // .odp
}

// kindForMime maps a MIME type to the job kind that renders it, or "" when the
// type gets no preview. Strips parameters (e.g. "; charset=utf-8") first.
func kindForMime(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	m = strings.TrimSpace(m)
	switch {
	case m == "application/pdf":
		return KindRenderPDF
	case m == "image/png", m == "image/jpeg", m == "image/gif", m == "image/webp":
		return KindRenderImage
	case m == "video/mp4", m == "video/webm", m == "video/quicktime":
		return KindRenderVideo
	case officeMimes[m]:
		return KindRenderOffice
	// .epub is a zip — a raw byte snippet is garbage, so it gets no preview
	// and is opened in the office editor instead.
	case m == "application/rtf", strings.HasPrefix(m, "text/"):
		return KindSnippetText
	default:
		return ""
	}
}

// previewKindFor maps a job kind to the artifact it produces. Every renderer
// that rasterizes produces a thumbnail — that collapse is the whole point of
// splitting the two kinds.
func previewKindFor(jobKind string) string {
	switch jobKind {
	case KindRenderImage, KindRenderPDF, KindRenderVideo, KindRenderOffice:
		return PreviewThumbnail
	case KindSnippetText:
		return PreviewText
	default:
		return ""
	}
}

// Run loops claiming and processing jobs until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if len(w.kinds) == 0 {
		slog.Info("preview worker idle: no renderers registered")
		return
	}
	slog.Info("preview worker started", "kinds", w.kinds)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.drain(ctx)
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	w.reap(ctx)

	jobs, err := w.q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: w.kinds, Lim: 5})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("claim jobs", "err", err)
		}
		return
	}
	for _, job := range jobs {
		if err := w.process(ctx, job); err != nil {
			slog.Warn("job failed", "kind", job.Kind, "attempt", job.Attempts, "err", err)
			if job.Attempts >= maxAttempts {
				_ = w.q.FailJob(ctx, store.FailJobParams{ID: job.ID, LastError: pgtype.Text{String: err.Error(), Valid: true}})
			} else {
				_ = w.q.RequeueJob(ctx, store.RequeueJobParams{ID: job.ID, LastError: pgtype.Text{String: err.Error(), Valid: true}})
			}
			continue
		}
		_ = w.q.CompleteJob(ctx, job.ID)
	}
}

// reap returns jobs abandoned by a dead worker to the queue.
//
// Nothing else does this: ClaimJobs marks a row 'running' and only the worker
// that claimed it ever moves it again. Lose that worker — OOM, container stop,
// a deploy mid-render — and the job is stranded silently.
//
// Deliberately not gated on w.kinds. A stranded render:pdf must be recoverable
// even by a process that cannot render PDFs, or losing the only worker that
// could do the work also loses the ability to notice.
func (w *Worker) reap(ctx context.Context) {
	n, err := w.q.ReapStuckJobs(ctx, store.ReapStuckJobsParams{
		MaxAttempts:  maxAttempts,
		LeaseSeconds: int32(jobLease.Seconds()),
	})
	if err != nil {
		slog.Warn("reap stuck jobs", "err", err)
		return
	}
	if n > 0 {
		slog.Warn("reclaimed jobs abandoned by a stopped worker", "count", n)
	}
}

func (w *Worker) process(ctx context.Context, job store.Job) error {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	var versionID pgtype.UUID
	if err := versionID.Scan(p.NodeVersionID); err != nil {
		return fmt.Errorf("bad version id: %w", err)
	}

	// ClaimJobs filters on w.kinds, so this can only miss if a kind was
	// unregistered between the claim and here — requeue rather than fail.
	renderer, ok := w.renderers[job.Kind]
	if !ok {
		return fmt.Errorf("no renderer for kind %q", job.Kind)
	}
	previewKind := previewKindFor(job.Kind)
	if previewKind == "" {
		return fmt.Errorf("no preview kind for job kind %q", job.Kind)
	}

	var src io.Reader = bytes.NewReader(nil)
	if sf, ok := renderer.(selfFetching); !ok || !sf.FetchesOwnSource() {
		rc, err := w.blobs.Get(ctx, p.BlobHash)
		if err != nil {
			return fmt.Errorf("get blob: %w", err)
		}
		defer rc.Close()
		src = rc
	}

	data, err := renderer.Render(ctx, src, p)
	if err != nil {
		return err
	}

	hash, _, err := w.blobs.Put(ctx, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("put preview blob: %w", err)
	}
	return w.q.UpsertPreview(ctx, store.UpsertPreviewParams{
		NodeVersionID: versionID,
		Kind:          previewKind,
		BlobHash:      hash,
	})
}

// GetPreviewBlob returns the preview blob reader for a version+kind, along
// with its content hash. The hash is the ETag: previews are content-addressed,
// so it is an exact validator for free. A nil reader means "not generated".
func (w *Worker) GetPreviewBlob(ctx context.Context, versionID pgtype.UUID, kind string) (io.ReadCloser, string, error) {
	p, err := w.q.GetPreview(ctx, store.GetPreviewParams{NodeVersionID: versionID, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	rc, err := w.blobs.Get(ctx, p.BlobHash)
	if err != nil {
		return nil, "", err
	}
	return rc, p.BlobHash, nil
}

func uuidStr(id pgtype.UUID) string {
	s, _ := id.Value()
	if s == nil {
		return ""
	}
	str, _ := s.(string)
	return str
}
