package preview

import (
	"bytes"
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
// plain text are pure Go and run in cmd/server. PDF and office documents run
// poppler and OnlyOffice, so their extractors (extract_pdf.go,
// extract_office.go) are registered in cmd/worker, beside the renderers that
// need the same things; until one exists their jobs wait, and storage,
// backfill and search are the same for every kind.
//
// The same job indexes a block document's page projections (M43): its
// page_meta (frontmatter icon and cover) and page_refs (sub-page blocks and
// page links). Both read the one parse the text needs, both are keyed to the
// current version under the same guard, and both have to follow the same
// events — a commit, a restore, a backfill — so a separate job kind would
// only parse the document twice.
const KindExtractText = "extract:text"

// Extractor turns a file's bytes into searchable plain text.
type Extractor interface {
	// Kind is the job kind this extractor handles, e.g. KindExtractText.
	Kind() string
	// Extract reads the original file and returns its text. Normalising,
	// capping and folding are the caller's job (textsearch.Prepare).
	Extract(ctx context.Context, src io.Reader, p Payload) (Extraction, error)
}

// Extraction is what an Extractor read from one version.
type Extraction struct {
	Text string
	// Page is set for a block document: what its page projections hold.
	Page *PageIndex
}

// PageIndex is a block document's page metadata and references, as stored
// in page_meta and page_refs.
type PageIndex struct {
	Icon, Cover     string
	SubPages, Links []string
}

// extractableMimes maps each base MIME type that has searchable text to the
// job kind that extracts it. Every office type that gets a thumbnail is
// indexed, so the two lists cannot drift apart.
var extractableMimes = func() map[string]string {
	m := map[string]string{
		"text/markdown":   KindExtractText,
		"text/plain":      KindExtractText,
		"application/pdf": KindExtractPDF,
	}
	for mime := range officeMimes {
		m[mime] = KindExtractOffice
	}
	return m
}()

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

// Reindex schedules text extraction (and a block document's page index) for
// a version that became current without being committed — a restored older
// version (fs.RestoreHook). Previews need nothing: the old version kept its
// own.
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

// extract runs an extraction job and stores its text (and a block document's
// page index) against the version — which the upserts do only while the
// version is still current, so a job that lost the race to a newer version is
// a no-op, not an error.
func (w *Worker) extract(ctx context.Context, e Extractor, p Payload, versionID pgtype.UUID) error {
	var nodeID pgtype.UUID
	if err := nodeID.Scan(p.NodeID); err != nil {
		return fmt.Errorf("bad node id: %w", err)
	}
	var src io.Reader = bytes.NewReader(nil)
	if sf, ok := e.(selfFetching); !ok || !sf.FetchesOwnSource() {
		rc, err := w.blobs.Get(ctx, p.BlobHash)
		if err != nil {
			return fmt.Errorf("get blob: %w", err)
		}
		defer rc.Close()
		src = rc
	}

	ex, err := e.Extract(ctx, src, p)
	if err != nil {
		return err
	}
	content, folded := textsearch.Prepare(ex.Text)
	if _, err := w.q.UpsertNodeText(ctx, store.UpsertNodeTextParams{
		NodeID:        nodeID,
		NodeVersionID: versionID,
		Content:       content,
		Folded:        folded,
	}); err != nil {
		return err
	}
	if ex.Page == nil {
		return nil
	}
	return indexPage(ctx, w.q, nodeID, versionID, *ex.Page)
}

// indexPage stores a block document's page_meta and page_refs for one
// version, in one transaction and only while that version is current: a
// stale version writes nothing. Reference ids that are not UUIDs (a
// hand-written pageId) are not pages and are skipped.
func indexPage(ctx context.Context, q *store.Queries, nodeID, versionID pgtype.UUID, page PageIndex) error {
	return q.InTx(ctx, func(q *store.Queries) error {
		n, err := q.UpsertPageMeta(ctx, store.UpsertPageMetaParams{
			NodeID:        nodeID,
			NodeVersionID: versionID,
			Icon:          pgtype.Text{String: page.Icon, Valid: page.Icon != ""},
			Cover:         pgtype.Text{String: page.Cover, Valid: page.Cover != ""},
		})
		if err != nil || n == 0 {
			return err
		}
		if _, err := q.DeletePageRefsForSource(ctx, store.DeletePageRefsForSourceParams{
			SourceID:      nodeID,
			NodeVersionID: versionID,
		}); err != nil {
			return err
		}
		_, err = q.InsertPageRefs(ctx, store.InsertPageRefsParams{
			SourceID:      nodeID,
			NodeVersionID: versionID,
			SubIds:        pageUUIDs(page.SubPages),
			LinkIds:       pageUUIDs(page.Links),
		})
		return err
	})
}

// pageUUIDs keeps the ids that are UUIDs — never nil, which pgx would send
// as a NULL array.
func pageUUIDs(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if u.Scan(id) == nil {
			out = append(out, u)
		}
	}
	return out
}

// maxMarkdownSource bounds how much of a markdown file is parsed. The
// document has to be parsed whole to know what is text and what is syntax;
// past this it is a data dump, not a document, and only its head is indexed.
const maxMarkdownSource = 8 << 20

// TextExtractor extracts markdown (through the block-document parser, so
// syntax, attachment references and comment markers stay out of the index,
// and with the document's page index) and plain text (verbatim).
type TextExtractor struct{}

func (TextExtractor) Kind() string { return KindExtractText }

func (TextExtractor) Extract(_ context.Context, src io.Reader, p Payload) (Extraction, error) {
	if baseMime(p.Mime) != "text/markdown" {
		// Prepare drops a rune the cap splits in half.
		b, err := io.ReadAll(io.LimitReader(src, textsearch.MaxIndexedBytes))
		return Extraction{Text: string(b)}, err
	}
	b, err := io.ReadAll(io.LimitReader(src, maxMarkdownSource))
	if err != nil {
		return Extraction{}, err
	}
	parsed, err := mdoc.Parse(b)
	if err != nil {
		return Extraction{}, fmt.Errorf("parse markdown: %w", err)
	}
	page := PageIndex{}
	page.Icon, page.Cover = mdoc.FrontmatterMeta(parsed.Frontmatter)
	page.SubPages, page.Links = mdoc.PageRefs(parsed.Doc)
	return Extraction{Text: mdoc.PlainText(parsed.Doc), Page: &page}, nil
}
