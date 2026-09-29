package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Attachments are the images and files dropped into a block document (M39).
// They belong to the document rather than to its folder: a row in
// node_attachments per blob, counted against the document's space like a
// version, released to GC by trigger when the document is purged.

// AddStagedAttachment makes a finished upload's staged bytes an attachment of
// docID, named name. The blob store takes the file over (blob.Store.Ingest);
// on failure it is left at path for the caller. The same bytes attached to the
// same document again are the attachment already there.
func (s *Service) AddStagedAttachment(ctx context.Context, docID pgtype.UUID, name, path string, creator pgtype.UUID) (store.NodeAttachment, error) {
	doc, err := s.get(ctx, docID)
	if err != nil {
		return store.NodeAttachment{}, err
	}
	if doc.Kind != store.NodeKindFile {
		return store.NodeAttachment{}, ErrNotFile
	}
	// Claimed by the store before it is installed, so an attachment refused
	// below (quota, a crash) leaves a row GC reclaims, never an orphan file.
	hash, size, err := s.blobs.Ingest(ctx, path)
	if err != nil {
		return store.NodeAttachment{}, fmt.Errorf("blob ingest: %w", err)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	var att store.NodeAttachment
	err = s.inTx(ctx, func(s *Service) error {
		existing, err := s.q.GetAttachment(ctx, store.GetAttachmentParams{NodeID: doc.ID, BlobHash: hash})
		if err == nil {
			att = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Measured, like a version's: the declared length was checked when
		// the upload opened, and this is the figure that actually landed.
		if err := s.EnsureRoom(ctx, doc.SpaceID, size); err != nil {
			return err
		}
		att, err = s.q.AddAttachment(ctx, store.AddAttachmentParams{
			NodeID:    doc.ID,
			BlobHash:  hash,
			Name:      name,
			Mime:      mimeType,
			Size:      size,
			CreatedBy: creator,
		})
		return err
	})
	if err != nil {
		return store.NodeAttachment{}, err
	}
	if err := s.RecountUsage(ctx, doc.SpaceID); err != nil {
		slog.WarnContext(ctx, "usage recount failed", "space", doc.SpaceID, "err", err)
	}
	return att, nil
}

// OpenAttachment returns docID's attachment with the given hash and a reader
// for its bytes. A hash the document does not hold is ErrNotFound, whether or
// not some other document holds it: the document is the whole authorization.
func (s *Service) OpenAttachment(ctx context.Context, docID pgtype.UUID, hash string) (store.NodeAttachment, io.ReadCloser, error) {
	att, err := s.q.GetAttachment(ctx, store.GetAttachmentParams{NodeID: docID, BlobHash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.NodeAttachment{}, nil, ErrNotFound
	}
	if err != nil {
		return store.NodeAttachment{}, nil, err
	}
	rc, err := s.blobs.Get(ctx, att.BlobHash)
	if err != nil {
		if errors.Is(err, blob.ErrNotExist) {
			return store.NodeAttachment{}, nil, fmt.Errorf("attachment blob %s missing", att.BlobHash[:12])
		}
		return store.NodeAttachment{}, nil, err
	}
	return att, rc, nil
}
