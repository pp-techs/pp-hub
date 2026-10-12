package fs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// ResolveRelative finds the node a relative reference in doc names, given
// its segments (mdoc.RelativePath). The path starts where doc sits: its
// parent — a folder, or the document a sub-page hangs under — and `..`
// climbs from there, so `../images/a.png` in a sub-page means what it means
// in the export, where a page's sub-pages sit in a folder beside it. Each name
// is a live file or folder; anything that does not resolve is ErrNotFound.
// Authorization is the caller's.
func (s *Service) ResolveRelative(ctx context.Context, doc store.Node, segs []string) (store.Node, error) {
	if len(segs) == 0 {
		return store.Node{}, ErrNotFound
	}
	dir := doc.ParentID
	for i, seg := range segs {
		if seg == ".." {
			if !dir.Valid {
				return store.Node{}, ErrNotFound
			}
			parent, err := s.q.GetNodeParent(ctx, dir)
			if errors.Is(err, pgx.ErrNoRows) {
				return store.Node{}, ErrNotFound
			}
			if err != nil {
				return store.Node{}, err
			}
			dir = parent
			continue
		}
		n, err := s.q.GetLiveChildByName(ctx, store.GetLiveChildByNameParams{SpaceID: doc.SpaceID, ParentID: dir, Name: seg})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Node{}, ErrNotFound
		}
		if err != nil {
			return store.Node{}, err
		}
		if i == len(segs)-1 {
			return n, nil
		}
		dir = n.ID
	}
	return store.Node{}, ErrNotFound
}

// InDocumentFolder reports whether target lies under the folder doc sits in —
// its nearest folder ancestor, through any documents it is a sub-page of.
// That subtree is how far a document's relative references reach on its
// behalf. A document with no folder above it reaches nothing this way.
func (s *Service) InDocumentFolder(ctx context.Context, doc, target store.Node) (bool, error) {
	cur := doc.ParentID
	for depth := 0; depth < maxTreeDepth && cur.Valid; depth++ {
		n, err := s.get(ctx, cur)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if n.Kind == store.NodeKindDir {
			return s.isDescendant(ctx, target.ID, n.ID)
		}
		cur = n.ParentID
	}
	return false, nil
}

// AttachFile makes file's current version one of docID's attachments: a
// relative reference embedded into the document, so it no longer follows
// the file. The caller has decided creator may download file; the bytes are
// the version's blob, so this is a row and a reference, billed to docID's
// space. The same bytes attached again are the attachment already there.
func (s *Service) AttachFile(ctx context.Context, docID pgtype.UUID, file store.Node, creator pgtype.UUID) (store.NodeAttachment, error) {
	doc, err := s.attachmentDoc(ctx, docID)
	if err != nil {
		return store.NodeAttachment{}, err
	}
	if file.Kind != store.NodeKindFile {
		return store.NodeAttachment{}, ErrNotFile
	}
	ver, err := s.q.GetCurrentVersion(ctx, file.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.NodeAttachment{}, ErrNotFound
	}
	if err != nil {
		return store.NodeAttachment{}, err
	}
	return s.addAttachment(ctx, doc, store.NodeAttachment{
		BlobHash: ver.BlobHash, Name: file.Name, Mime: attachmentMime(file.Name), Size: ver.Size,
	}, creator)
}
