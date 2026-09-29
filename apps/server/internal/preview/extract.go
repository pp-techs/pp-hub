package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/textsearch"
)

// Content search (M27) rides the same jobs table and claim loop as previews,
// but produces a node_texts row instead of a preview blob.
//
// An extraction job kind is a routing key exactly like a render kind: a
// process claims only the kinds it registered an Extractor for. Markdown and
// plain text are pure Go and run in cmd/server. Office documents will need
// their own kind (their extractor has to live in cmd/worker, beside the
// converter), added to extractKindForMime with its Extractor registered there;
// storage, backfill and search need no change.
const KindExtractText = "extract:text"

// Extractor turns a file's bytes into searchable plain text.
type Extractor interface {
	// Kind is the job kind this extractor handles, e.g. KindExtractText.
	Kind() string
	// Extract reads the original file and returns its text. Normalising,
	// capping and folding are the caller's job (textsearch.Prepare).
	Extract(ctx context.Context, src io.Reader, p Payload) (string, error)
}

// extractableMimes maps each base MIME type that has searchable text to the
// job kind that extracts it.
var extractableMimes = map[string]string{
	"text/markdown": KindExtractText,
	"text/plain":    KindExtractText,
}

// extractKindForMime maps a MIME type to its extraction job kind, or "" when
// the type's content is not indexed.
func extractKindForMime(m string) string {
	return extractableMimes[baseMime(m)]
}

func baseMime(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.ToLower(strings.TrimSpace(m))
}

// WithExtractors registers the extractors this process can run, alongside its
// renderers. Call before Run.
func (w *Worker) WithExtractors(es ...Extractor) *Worker {
	for _, e := range es {
		w.extractors[e.Kind()] = e
	}
	w.kinds = w.deriveKinds()
	return w
}

// Reindex schedules text extraction for a version that became current
// without being committed — a restored older version (fs.RestoreHook).
// Previews need nothing: the old version kept its own.
func (w *Worker) Reindex(ctx context.Context, ver store.NodeVersion) error {
	return w.enqueue(ctx, extractKindForMime(ver.Mime), ver, "")
}

func (w *Worker) enqueue(ctx context.Context, kind string, ver store.NodeVersion, name string) error {
	if kind == "" {
		return nil
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

// extract runs an extraction job and stores its text against the version —
// which UpsertNodeText does only while the version is still current, so a
// job that lost the race to a newer version is a no-op, not an error.
func (w *Worker) extract(ctx context.Context, e Extractor, p Payload, versionID pgtype.UUID) error {
	var nodeID pgtype.UUID
	if err := nodeID.Scan(p.NodeID); err != nil {
		return fmt.Errorf("bad node id: %w", err)
	}
	rc, err := w.blobs.Get(ctx, p.BlobHash)
	if err != nil {
		return fmt.Errorf("get blob: %w", err)
	}
	defer rc.Close()

	text, err := e.Extract(ctx, rc, p)
	if err != nil {
		return err
	}
	content, folded := textsearch.Prepare(text)
	_, err = w.q.UpsertNodeText(ctx, store.UpsertNodeTextParams{
		NodeID:        nodeID,
		NodeVersionID: versionID,
		Content:       content,
		Folded:        folded,
	})
	return err
}

// maxMarkdownSource bounds how much of a markdown file is parsed. The
// document has to be parsed whole to know what is text and what is syntax;
// past this it is a data dump, not a document, and only its head is indexed.
const maxMarkdownSource = 8 << 20

// TextExtractor extracts markdown (through the block-document parser, so
// syntax, attachment references and comment markers stay out of the index)
// and plain text (verbatim).
type TextExtractor struct{}

func (TextExtractor) Kind() string { return KindExtractText }

func (TextExtractor) Extract(_ context.Context, src io.Reader, p Payload) (string, error) {
	if baseMime(p.Mime) != "text/markdown" {
		// Prepare drops a rune the cap splits in half.
		b, err := io.ReadAll(io.LimitReader(src, textsearch.MaxIndexedBytes))
		return string(b), err
	}
	b, err := io.ReadAll(io.LimitReader(src, maxMarkdownSource))
	if err != nil {
		return "", err
	}
	parsed, err := mdoc.Parse(b)
	if err != nil {
		return "", fmt.Errorf("parse markdown: %w", err)
	}
	return mdoc.PlainText(parsed.Doc), nil
}
