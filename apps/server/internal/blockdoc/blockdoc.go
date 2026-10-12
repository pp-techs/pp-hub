// Package blockdoc is what the server does with a block document (M39) beyond
// editing it live: reading a version as the ProseMirror document a read-only
// view renders, exporting it with its attachments, and the templates a new one
// starts from. Editing is internal/collab's; the markdown itself is
// internal/mdoc's. See docs/architecture/block-documents.md.
package blockdoc

import (
	"context"
	"errors"
	"io"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

var (
	// ErrNotMarkdown refuses a version that is not a block document.
	ErrNotMarkdown = errors.New("this file is not a markdown document")
	// ErrTooLarge refuses a document too big to parse: a live Y.Doc costs a
	// multiple of its markdown, and so does the tree a view renders.
	ErrTooLarge = errors.New("this document is too large to open in the editor")
)

// MaxBytes bounds the markdown the server parses — for a room, a view or an
// export. Nobody writes a 16 MiB document by hand.
const MaxBytes = 16 << 20

// Parse reads a markdown version's bytes and parses them. A version that is
// not markdown is ErrNotMarkdown, one over MaxBytes ErrTooLarge.
func Parse(ver store.NodeVersion, r io.Reader) (mdoc.Parsed, error) {
	src, err := readSource(ver, r)
	if err != nil {
		return mdoc.Parsed{}, err
	}
	return mdoc.Parse(src)
}

// readSource reads a markdown version's bytes, judging MaxBytes by the
// recorded size and again by what the reader actually yields.
func readSource(ver store.NodeVersion, r io.Reader) ([]byte, error) {
	if !fs.IsMarkdown(ver.Mime) {
		return nil, ErrNotMarkdown
	}
	if ver.Size > MaxBytes {
		return nil, ErrTooLarge
	}
	src, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(src) > MaxBytes {
		return nil, ErrTooLarge
	}
	return src, nil
}

// Files is the file tree documents are read from (fs.Service). Its errors are
// fs's.
type Files interface {
	OpenFile(ctx context.Context, nodeID pgtype.UUID) (store.Node, store.NodeVersion, io.ReadCloser, error)
	OpenVersion(ctx context.Context, nodeID, versionID pgtype.UUID) (store.NodeVersion, io.ReadCloser, error)
	ListAttachments(ctx context.Context, docID pgtype.UUID) ([]store.NodeAttachment, error)
	OpenAttachment(ctx context.Context, docID pgtype.UUID, hash string) (store.NodeAttachment, io.ReadCloser, error)
	// Tree and PageTitles serve an export: the pages it holds and the
	// titles of those it links to elsewhere. ResolveRelative finds the
	// file a page's relative reference names.
	Tree(ctx context.Context, root store.Node) (*fs.TreeNode, error)
	PageTitles(ctx context.Context, space pgtype.UUID, ids []string) (map[string]string, error)
	ResolveRelative(ctx context.Context, doc store.Node, segs []string) (store.Node, error)
}

// Service reads and exports block documents. Authorization is the caller's.
type Service struct {
	files Files
}

// New builds the service over files.
func New(files Files) *Service { return &Service{files: files} }

// Document is one version of a block document, as the editor's schema sees it.
type Document struct {
	Doc     mdoc.Node
	Lossy   bool
	Version int32
}

// Document parses a version of nodeID: the current one when versionID is not
// valid, otherwise that version, which must be nodeID's (fs.ErrNotFound
// otherwise).
func (s *Service) Document(ctx context.Context, nodeID, versionID pgtype.UUID) (Document, error) {
	var (
		ver store.NodeVersion
		rc  io.ReadCloser
		err error
	)
	if versionID.Valid {
		ver, rc, err = s.files.OpenVersion(ctx, nodeID, versionID)
	} else {
		_, ver, rc, err = s.files.OpenFile(ctx, nodeID)
	}
	if err != nil {
		return Document{}, err
	}
	defer rc.Close()
	parsed, err := Parse(ver, rc)
	if err != nil {
		return Document{}, err
	}
	return Document{Doc: parsed.Doc, Lossy: parsed.Lossy, Version: ver.VersionNo}, nil
}
