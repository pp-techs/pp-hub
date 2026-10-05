package fs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// maxCopiedPageBytes bounds the markdown a copy reads to map its page ids:
// blockdoc.MaxBytes, which fs cannot import. A larger document is past what
// the editor opens, and its copy keeps sharing the original's bytes.
const maxCopiedPageBytes = 16 << 20

// copyOp is what one Copy has made so far, inside its transaction.
type copyOp struct {
	space   pgtype.UUID
	creator pgtype.UUID
	// ids maps every block document copied to its copy.
	ids map[[16]byte]pgtype.UUID
	// pages are the copied block documents still without a version, in the
	// order they were copied.
	pages []copiedPage
	// versions are the versions written, for the version hook after commit.
	versions []copiedVersion
}

type copiedPage struct {
	node store.Node        // the copy
	src  store.NodeVersion // the original's current version
}

type copiedVersion struct {
	ver  store.NodeVersion
	name string
}

// copySubPages copies src's live sub-pages under dst, its copy, in page-tree
// order.
func (s *Service) copySubPages(ctx context.Context, op *copyOp, src, dst store.Node, depth int) error {
	children, err := s.q.ListChildren(ctx, store.ListChildrenParams{SpaceID: src.SpaceID, ParentID: src.ID})
	if err != nil {
		return err
	}
	// ListChildren sorts by name; the page tree reads created_at first.
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].CreatedAt.Time.Before(children[j].CreatedAt.Time)
	})
	for _, child := range children {
		// Only a document hangs under a document (CanContain). Anything else
		// there would be refused under the copy, so it is left behind.
		page, err := s.isPage(ctx, child)
		if err != nil {
			return err
		}
		if !page {
			continue
		}
		copied, err := s.copyInto(ctx, op, child, dst.ID, child.Name, depth+1)
		if err != nil {
			return err
		}
		if err := s.q.StampCopiedPage(ctx, copied.ID); err != nil {
			return err
		}
	}
	return nil
}

// writeCopiedPages gives every copied block document its version. One whose
// sub-page blocks or page links point at a document copied in the same
// operation is rewritten to point at that copy — the copy of a page tree is a
// tree of its own, not one that leads back into the original — and stored as
// a new blob. Links outside the copied set stay as they are, and a document
// with nothing to map keeps sharing its original's blob.
func (s *Service) writeCopiedPages(ctx context.Context, op *copyOp) error {
	type render struct {
		page        copiedPage
		src         []byte
		doc         mdoc.Node
		frontmatter []byte
	}
	var renders []render
	var titled []string
	for _, p := range op.pages {
		src, err := s.copiedPageSource(ctx, p.src)
		if err != nil {
			return err
		}
		var mapped map[string]string
		var parsed mdoc.Parsed
		if src != nil {
			// Parse never fails on bytes; an error would be a parser bug,
			// and the copy then keeps the original's bytes untouched.
			if parsed, err = mdoc.Parse(src); err == nil {
				mapped = op.mapPageIDs(mdoc.PageRefs(parsed.Doc))
			}
		}
		switch {
		case len(mapped) == 0:
			if err := s.addCopyVersion(ctx, op, p.node, p.src.BlobHash, p.src.Size, p.src.Mime); err != nil {
				return err
			}
		case parsed.Lossy:
			// Rendering a lossy document writes what the block set can hold
			// and drops the rest — a copy must not be the thing that loses
			// it. The ids are rewritten in the bytes instead: a UUID is 36
			// fixed characters, so one cannot be part of another, and the
			// `/edit/<id>` hrefs and `doc:<id>` mentions follow the markers.
			out := src
			for from, to := range mapped {
				out = bytes.ReplaceAll(out, []byte(from), []byte(to))
			}
			if err := s.putCopiedPage(ctx, op, p, src, out); err != nil {
				return err
			}
		default:
			doc := mdoc.MapPageIDs(parsed.Doc, func(id string) string { return mapped[id] })
			subs, links := mdoc.PageRefs(doc)
			titled = append(append(titled, subs...), links...)
			renders = append(renders, render{page: p, src: src, doc: doc, frontmatter: parsed.Frontmatter})
		}
	}
	if len(renders) == 0 {
		return nil
	}
	// Titles are read inside the transaction, so a copy's own title is
	// its new name — "Plan (2)" beside the original — not the original's.
	titles, err := s.PageTitles(ctx, op.space, titled)
	if err != nil {
		return err
	}
	opts := mdoc.RenderOptions{PageTitle: func(id string) string { return titles[id] }}
	for _, r := range renders {
		if err := s.putCopiedPage(ctx, op, r.page, r.src, mdoc.RenderWith(r.doc, r.frontmatter, opts)); err != nil {
			return err
		}
	}
	return nil
}

// mapPageIDs answers, for the page ids a document refers to, those copied in
// op, keyed by the id as the document writes it and mapped to the copy's id.
func (op *copyOp) mapPageIDs(subPages, links []string) map[string]string {
	var mapped map[string]string
	for _, ids := range [][]string{subPages, links} {
		for _, id := range ids {
			var u pgtype.UUID
			if u.Scan(id) != nil {
				continue
			}
			if c, ok := op.ids[u.Bytes]; ok {
				if mapped == nil {
					mapped = map[string]string{}
				}
				mapped[id] = c.String()
			}
		}
	}
	return mapped
}

// copiedPageSource reads a copied document's markdown, or answers nil when
// it is too large to parse or its bytes are missing: the copy then shares the
// original's blob as any other file's copy does.
func (s *Service) copiedPageSource(ctx context.Context, ver store.NodeVersion) ([]byte, error) {
	if ver.Size > maxCopiedPageBytes {
		return nil, nil
	}
	rc, err := s.blobs.Get(ctx, ver.BlobHash)
	if errors.Is(err, blob.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	src, err := io.ReadAll(io.LimitReader(rc, maxCopiedPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(src) > maxCopiedPageBytes {
		return nil, nil
	}
	return src, nil
}

// putCopiedPage stores out as the copy's version, or shares the original's
// blob when out is the original's bytes after all.
//
// Put runs inside Copy's transaction, and the blob store claims the hash on a
// connection of its own. That claim cannot wait on this transaction: out
// carries ids minted in it, so no blob row this transaction has touched holds
// that hash. The version row is the reference and its size the figure usage
// counts, as putFile records it.
func (s *Service) putCopiedPage(ctx context.Context, op *copyOp, p copiedPage, src, out []byte) error {
	if bytes.Equal(out, src) {
		return s.addCopyVersion(ctx, op, p.node, p.src.BlobHash, p.src.Size, p.src.Mime)
	}
	hash, size, err := s.blobs.Put(ctx, bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("blob put: %w", err)
	}
	return s.addCopyVersion(ctx, op, p.node, hash, size, p.src.Mime)
}
