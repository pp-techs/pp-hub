package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Attachments are the images, videos and files dropped into a block document
// (M39). They belong to the document rather than to its folder: a row in
// node_attachments per blob, counted against the document's space like a
// version, released to GC by trigger when the document is purged.

// attachmentMimes are the types an attachment's name maps to that Go's builtin
// table lacks. Without them a video's type came from the host's mime.types —
// video/mp4 on one machine, application/octet-stream on a slim container —
// and whether a document's video played in place or downloaded depended on
// which one stored it.
var attachmentMimes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
}

// attachmentMime is the stored type of an attachment named name.
func attachmentMime(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := attachmentMimes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// AddStagedAttachment makes a finished upload's staged bytes an attachment of
// docID, named name. The blob store takes the file over (blob.Store.Ingest);
// on failure it is left at path for the caller. The same bytes attached to the
// same document again are the attachment already there.
func (s *Service) AddStagedAttachment(ctx context.Context, docID pgtype.UUID, name, path string, creator pgtype.UUID) (store.NodeAttachment, error) {
	doc, err := s.attachmentDoc(ctx, docID)
	if err != nil {
		return store.NodeAttachment{}, err
	}
	// Claimed by the store before it is installed, so an attachment refused
	// below (quota, a crash) leaves a row GC reclaims, never an orphan file.
	hash, size, err := s.blobs.Ingest(ctx, path)
	if err != nil {
		return store.NodeAttachment{}, fmt.Errorf("blob ingest: %w", err)
	}
	return s.addAttachment(ctx, doc, store.NodeAttachment{BlobHash: hash, Name: name, Mime: attachmentMime(name), Size: size}, creator)
}

// PutAttachment stores r as an attachment of docID, named name — bytes the
// server fetched itself (a link preview's image), which never went through an
// upload. The caller bounds r; the quota is checked here as for an upload.
func (s *Service) PutAttachment(ctx context.Context, docID pgtype.UUID, name string, r io.Reader, creator pgtype.UUID) (store.NodeAttachment, error) {
	doc, err := s.attachmentDoc(ctx, docID)
	if err != nil {
		return store.NodeAttachment{}, err
	}
	hash, size, err := s.blobs.Put(ctx, r)
	if err != nil {
		return store.NodeAttachment{}, fmt.Errorf("blob put: %w", err)
	}
	return s.addAttachment(ctx, doc, store.NodeAttachment{BlobHash: hash, Name: name, Mime: attachmentMime(name), Size: size}, creator)
}

// AdoptAttachment makes another document's attachment one of docID's too: an
// image pasted from one document into another names a hash only the first
// holds. The caller has decided the adopter may read it; the bytes are the
// same blob, so this is a row and a reference, billed to docID's space.
func (s *Service) AdoptAttachment(ctx context.Context, docID pgtype.UUID, from store.NodeAttachment, creator pgtype.UUID) (store.NodeAttachment, error) {
	doc, err := s.attachmentDoc(ctx, docID)
	if err != nil {
		return store.NodeAttachment{}, err
	}
	return s.addAttachment(ctx, doc, from, creator)
}

// ListAttachments is every attachment docID holds, oldest first.
func (s *Service) ListAttachments(ctx context.Context, docID pgtype.UUID) ([]store.NodeAttachment, error) {
	return s.q.ListAttachments(ctx, docID)
}

// AttachmentHolders lists live documents other than exclude that hold hash,
// each with its attachment row — the documents an adopted reference may be
// copied from.
func (s *Service) AttachmentHolders(ctx context.Context, hash string, exclude pgtype.UUID) ([]store.ListAttachmentHoldersRow, error) {
	return s.q.ListAttachmentHolders(ctx, store.ListAttachmentHoldersParams{BlobHash: hash, ExcludeID: exclude})
}

func (s *Service) attachmentDoc(ctx context.Context, docID pgtype.UUID) (store.Node, error) {
	doc, err := s.get(ctx, docID)
	if err != nil {
		return store.Node{}, err
	}
	if doc.Kind != store.NodeKindFile {
		return store.Node{}, ErrNotFile
	}
	return doc, nil
}

// addAttachment records att's blob as doc's attachment, unless doc already
// holds it. The blob is claimed already (by the store that wrote it, or by
// the attachment it is adopted from).
func (s *Service) addAttachment(ctx context.Context, doc store.Node, att store.NodeAttachment, creator pgtype.UUID) (store.NodeAttachment, error) {
	var out store.NodeAttachment
	err := s.inTx(ctx, func(s *Service) error {
		existing, err := s.q.GetAttachment(ctx, store.GetAttachmentParams{NodeID: doc.ID, BlobHash: att.BlobHash})
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Measured, like a version's: the figure that actually landed (an
		// upload's declared length was checked when it opened), or the size
		// of the attachment being adopted.
		if err := s.EnsureRoom(ctx, doc.SpaceID, att.Size); err != nil {
			return err
		}
		out, err = s.q.AddAttachment(ctx, store.AddAttachmentParams{
			NodeID:    doc.ID,
			BlobHash:  att.BlobHash,
			Name:      att.Name,
			Mime:      att.Mime,
			Size:      att.Size,
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
	return out, nil
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
