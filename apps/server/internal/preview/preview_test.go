package preview

import (
	"context"
	"io"
	"slices"
	"testing"
)

// kindForMime picks the job kind that renders a MIME type. Unlike the old
// contract, this no longer has to mirror anything in the web client — the
// browser asks for a preview kind, which previewKindFor decides.
func TestKindForMime(t *testing.T) {
	cases := map[string]string{
		"application/pdf":         KindRenderPDF,
		"image/png":               KindRenderImage,
		"image/jpeg":              KindRenderImage,
		"image/gif":               KindRenderImage,
		"image/webp":              KindRenderImage,
		"image/svg+xml":           KindRenderSVG,
		"video/mp4":               KindRenderVideo,
		"video/webm":              KindRenderVideo,
		"video/quicktime":         KindRenderVideo,
		"application/rtf":         KindSnippetText,
		"text/plain":              KindSnippetText,
		"text/markdown":           KindSnippetText,
		"text/csv; charset=utf-8": KindSnippetText,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": KindRenderOffice,
		// No preview: .epub is a zip the editor opens but a cover render would
		// misrepresent; the rest have no renderer at all.
		"application/epub+zip": "",
		"video/x-matroska":     "",
		"application/zip":      "",
		"":                     "",
	}
	for mime, want := range cases {
		if got := kindForMime(mime); got != want {
			t.Errorf("kindForMime(%q) = %q, want %q", mime, got, want)
		}
	}
}

// Everything that rasterizes produces a thumbnail. This collapse is what frees
// the web client from mirroring kindForMime: apps/web asks for "thumbnail" for
// an image, a PDF and a video alike.
func TestPreviewKindFor(t *testing.T) {
	cases := map[string]string{
		KindRenderImage:  PreviewThumbnail,
		KindRenderSVG:    PreviewThumbnail,
		KindRenderPDF:    PreviewThumbnail,
		KindRenderVideo:  PreviewThumbnail,
		KindRenderOffice: PreviewThumbnail,
		KindSnippetText:  PreviewText,
		"render:unknown": "",
		"":               "",
	}
	for jobKind, want := range cases {
		if got := previewKindFor(jobKind); got != want {
			t.Errorf("previewKindFor(%q) = %q, want %q", jobKind, got, want)
		}
	}
}

// Every kind a MIME can map to must have a preview kind, or the job would be
// claimed and then fail in process() with "no preview kind".
func TestEveryJobKindHasPreviewKind(t *testing.T) {
	mimes := []string{
		"application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp",
		"image/svg+xml", "video/mp4", "video/webm", "video/quicktime",
		"application/rtf", "text/plain",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}
	for _, m := range mimes {
		jobKind := kindForMime(m)
		if jobKind == "" {
			t.Fatalf("kindForMime(%q) returned no job kind", m)
		}
		if previewKindFor(jobKind) == "" {
			t.Errorf("job kind %q (from %q) has no preview kind", jobKind, m)
		}
	}
}

type fakeRenderer struct{ kind string }

func (f fakeRenderer) Kind() string { return f.kind }
func (f fakeRenderer) Render(context.Context, io.Reader, Payload) ([]byte, error) {
	return []byte("x"), nil
}

// The kinds a worker claims are derived from the renderers it registered, so a
// process cannot advertise work it has no way of doing. This is the guarantee
// that makes cmd/worker optional — see TestClaimJobsFiltersByKind in
// jobs_db_test.go for the SQL half.
func TestWorkerKindsAreDerivedFromRenderers(t *testing.T) {
	w := NewWorker(nil, nil, fakeRenderer{KindRenderPDF}, fakeRenderer{KindRenderVideo})
	got := w.Kinds()
	want := []string{KindRenderPDF, KindRenderVideo}
	if !slices.Equal(got, want) {
		t.Errorf("Kinds() = %v, want %v", got, want)
	}
	if _, ok := w.renderers[KindRenderImage]; ok {
		t.Error("worker advertises render:image without registering it")
	}
}

// A worker with no renderers must claim nothing at all rather than claiming
// everything: ClaimJobs with an empty kinds array would match no rows, but
// Run short-circuits before even querying.
func TestWorkerWithNoRenderersClaimsNothing(t *testing.T) {
	w := NewWorker(nil, nil)
	if len(w.Kinds()) != 0 {
		t.Errorf("Kinds() = %v, want empty", w.Kinds())
	}
	// Run must return immediately rather than tick against a nil *Queries.
	done := make(chan struct{})
	go func() { w.Run(context.Background()); close(done) }()
	<-done
}
