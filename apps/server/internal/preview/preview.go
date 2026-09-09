// Package preview implements background job workers that generate file
// previews (image thumbnail, PDF first page, video probe, text snippet).
// See docs/architecture/milestones.md M5.
package preview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Kinds of jobs we process.
const (
	JobThumbnail = "thumbnail"
	JobPDF       = "pdf"
	JobVideo     = "video"
	JobText      = "text"
)

const maxAttempts = 5

// Worker claims jobs and generates previews.
type Worker struct {
	q     *store.Queries
	blobs blob.Store
}

func NewWorker(q *store.Queries, blobs blob.Store) *Worker {
	return &Worker{q: q, blobs: blobs}
}

// Payload for preview jobs.
type Payload struct {
	NodeVersionID string `json:"node_version_id"`
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
		BlobHash:      ver.BlobHash,
		Mime:          ver.Mime,
		Name:          name,
	})
	_, err := w.q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: kind, Payload: payload})
	return err
}

// kindForMime maps MIME types to preview job kinds. Strips parameters
// (e.g. "; charset=utf-8") before matching.
func kindForMime(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	m = strings.TrimSpace(m)
	switch {
	case m == "application/pdf":
		return JobPDF
	case m == "image/png", m == "image/jpeg", m == "image/gif", m == "image/webp":
		return JobThumbnail
	case m == "video/mp4", m == "video/webm", m == "video/quicktime":
		return JobVideo
	case m == "text/plain", m == "text/markdown", m == "text/csv":
		return JobText
	default:
		return ""
	}
}

// Run loops claiming and processing jobs until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
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
	jobs, err := w.q.ClaimJobs(ctx, 5)
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

func (w *Worker) process(ctx context.Context, job store.Job) error {
	var p Payload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	var versionID pgtype.UUID
	if err := versionID.Scan(p.NodeVersionID); err != nil {
		return fmt.Errorf("bad version id: %w", err)
	}

	rc, err := w.blobs.Get(ctx, p.BlobHash)
	if err != nil {
		return fmt.Errorf("get blob: %w", err)
	}
	defer rc.Close()

	var previewData []byte
	var previewKind string

	switch job.Kind {
	case JobThumbnail:
		previewData, err = imageThumbnail(rc)
		previewKind = "thumbnail"
	case JobPDF:
		previewData, err = pdfFirstPage(rc)
		previewKind = "pdf"
	case JobVideo:
		previewData, err = videoProbe(rc)
		previewKind = "video"
	case JobText:
		previewData, err = textSnippet(rc)
		previewKind = "text"
	default:
		return fmt.Errorf("unknown job kind %q", job.Kind)
	}
	if err != nil {
		return err
	}

	hash, _, err := w.blobs.Put(ctx, bytes.NewReader(previewData))
	if err != nil {
		return fmt.Errorf("put preview blob: %w", err)
	}
	return w.q.UpsertPreview(ctx, store.UpsertPreviewParams{
		NodeVersionID: versionID,
		Kind:          previewKind,
		BlobHash:      hash,
	})
}

// GetPreviewBlob returns the preview blob reader for a version+kind.
func (w *Worker) GetPreviewBlob(ctx context.Context, versionID pgtype.UUID, kind string) (io.ReadCloser, error) {
	p, err := w.q.GetPreview(ctx, store.GetPreviewParams{NodeVersionID: versionID, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return w.blobs.Get(ctx, p.BlobHash)
}

func uuidStr(id pgtype.UUID) string {
	s, _ := id.Value()
	return s.(string)
}
