// Package uploads keeps the state of resumable uploads: a row in `uploads`
// for each one and a staging file holding the bytes received so far.
//
// It knows nothing about the tus wire protocol (internal/httpapi speaks that)
// and nothing about files and versions (internal/fs commits the finished
// bytes). What it guarantees is the one thing both depend on: the offset in
// the row never claims a byte the staging file does not durably hold.
package uploads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// TTL is how long an upload survives without receiving a byte. Every
// accepted chunk slides it forward, so it bounds idleness, not duration: a
// laptop closed overnight can still finish in the morning, and an abandoned
// upload stops holding disk (and quota) a day later.
const TTL = 24 * time.Hour

// sweepBatch bounds one sweep's DELETE.
const sweepBatch = 200

var (
	// ErrNotFound covers unknown, expired, foreign and terminated uploads
	// alike: an upload is visible only to its creator, and telling "not
	// yours" apart from "gone" would say more than the caller may know.
	ErrNotFound = errors.New("upload not found")
	// ErrOffsetMismatch is a PATCH that does not continue where the upload
	// stands; the client is expected to ask (HEAD) and resume from there.
	ErrOffsetMismatch = errors.New("upload offset does not match")
	// ErrCompleted refuses bytes for an upload that has been committed.
	ErrCompleted = errors.New("upload already completed")
)

type Service struct {
	q   *store.Queries
	dir string

	mu   sync.Mutex
	busy map[[16]byte]chan struct{}
}

// NewService stages uploads under dir, creating it if needed.
func NewService(q *store.Queries, dir string) (*Service, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("upload staging dir: %w", err)
	}
	return &Service{q: q, dir: dir, busy: map[[16]byte]chan struct{}{}}, nil
}

// CreateParams describes a new upload. NodeID set means the bytes will become
// a new version of that file rather than a new file in ParentID; AttachTo set
// means they will become an attachment of that document.
type CreateParams struct {
	SpaceID   pgtype.UUID
	ParentID  pgtype.UUID
	NodeID    pgtype.UUID
	AttachTo  pgtype.UUID
	Name      string
	TotalSize int64
	CreatedBy pgtype.UUID
}

// Create records an upload and its empty staging file.
func (s *Service) Create(ctx context.Context, p CreateParams) (store.Upload, error) {
	name, err := stagingName()
	if err != nil {
		return store.Upload{}, err
	}
	// The file exists before the row does: a crash in between leaves a stray
	// empty file, never a row pointing at nothing.
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return store.Upload{}, err
	}
	if err := f.Close(); err != nil {
		return store.Upload{}, err
	}
	up, err := s.q.CreateUpload(ctx, store.CreateUploadParams{
		SpaceID:   p.SpaceID,
		ParentID:  p.ParentID,
		NodeID:    p.NodeID,
		AttachTo:  p.AttachTo,
		Name:      p.Name,
		TotalSize: p.TotalSize,
		TempPath:  name,
		ExpiresAt: expiry(),
		CreatedBy: p.CreatedBy,
	})
	if err != nil {
		_ = os.Remove(filepath.Join(s.dir, name))
		return store.Upload{}, err
	}
	return up, nil
}

// Get returns the caller's upload.
func (s *Service) Get(ctx context.Context, id, userID pgtype.UUID) (store.Upload, error) {
	up, err := s.q.GetUpload(ctx, store.GetUploadParams{ID: id, CreatedBy: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Upload{}, ErrNotFound
	}
	return up, err
}

// Lock serialises work on one upload within this process: two PATCHes for
// the same upload must not interleave writes, and a retried final PATCH has
// to wait for the commit already running rather than start a second one.
// Callers re-read the row after locking. It waits for ctx.
func (s *Service) Lock(ctx context.Context, id pgtype.UUID) (unlock func(), err error) {
	for {
		s.mu.Lock()
		held, busy := s.busy[id.Bytes]
		if !busy {
			done := make(chan struct{})
			s.busy[id.Bytes] = done
			s.mu.Unlock()
			return func() {
				s.mu.Lock()
				delete(s.busy, id.Bytes)
				s.mu.Unlock()
				close(done)
			}, nil
		}
		s.mu.Unlock()
		select {
		case <-held:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Path is where an upload's bytes are staged.
func (s *Service) Path(up store.Upload) string {
	return filepath.Join(s.dir, up.TempPath)
}

// Append writes body at offset and returns the new offset. Hold Lock.
//
// Bytes that arrived before body failed are kept and counted: a connection
// cut by a proxy mid-chunk loses only what never arrived, which is the point
// of the protocol. The returned error is body's, after the offset has been
// recorded. The data is fsynced before the row advances, so a crash can lose
// the tail of a chunk but never leave the row ahead of the file.
func (s *Service) Append(ctx context.Context, up store.Upload, offset int64, body io.Reader) (int64, error) {
	if up.CompletedAt.Valid {
		return 0, ErrCompleted
	}
	if offset != up.Uploaded {
		return 0, ErrOffsetMismatch
	}
	f, err := os.OpenFile(s.Path(up), os.O_WRONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return 0, s.discard(ctx, up, "staging file missing")
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() < offset {
		// The row promises bytes the disk does not have — a restore that
		// brought the database back without the staging directory, say.
		// Resuming would splice zeros into the file; starting over is the
		// only honest answer.
		return 0, s.discard(ctx, up, "staging file shorter than recorded offset")
	}
	// Anything past the offset is a tail from a write that never made it
	// into the row; the client is about to send those bytes again.
	if err := f.Truncate(offset); err != nil {
		return 0, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}

	n, copyErr := io.Copy(f, io.LimitReader(body, up.TotalSize-offset))
	if n == 0 {
		return offset, copyErr
	}
	if err := f.Sync(); err != nil {
		return offset, err
	}
	next := offset + n
	if err := s.q.AdvanceUpload(ctx, store.AdvanceUploadParams{ID: up.ID, Uploaded: next, ExpiresAt: expiry()}); err != nil {
		return offset, err
	}
	return next, copyErr
}

// Complete marks an upload committed. Its staging file is gone by now — the
// blob store took it — and the row stays until it expires, answering HEAD
// with a full offset for a client that missed the final response.
func (s *Service) Complete(ctx context.Context, id pgtype.UUID) error {
	return s.q.CompleteUpload(ctx, id)
}

// CompleteAttachment marks an attachment upload committed and records the
// hash its bytes became, which HEAD hands back to a client that missed the
// final response.
func (s *Service) CompleteAttachment(ctx context.Context, id pgtype.UUID, hash string) error {
	return s.q.CompleteAttachmentUpload(ctx, store.CompleteAttachmentUploadParams{
		ID: id, AttachmentHash: pgtype.Text{String: hash, Valid: true},
	})
}

// Terminate deletes an upload and its staged bytes.
func (s *Service) Terminate(ctx context.Context, up store.Upload) error {
	if err := s.q.DeleteUpload(ctx, up.ID); err != nil {
		return err
	}
	if err := os.Remove(s.Path(up)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// PendingBytes is what uploads still in flight have promised to a space.
func (s *Service) PendingBytes(ctx context.Context, spaceID pgtype.UUID) (int64, error) {
	return s.q.PendingUploadBytes(ctx, spaceID)
}

// Sweep removes uploads nobody has touched within TTL, and their bytes.
// Rows go first, files second: a file with no row is a leak the orphan pass
// below collects, a row with no file is an upload that silently cannot
// resume.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	total := 0
	for {
		paths, err := s.q.DeleteExpiredUploads(ctx, sweepBatch)
		if err != nil {
			return total, err
		}
		for _, p := range paths {
			if err := os.Remove(filepath.Join(s.dir, p)); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.WarnContext(ctx, "expired upload file not removed", "file", p, "err", err)
			}
		}
		total += len(paths)
		if len(paths) < sweepBatch {
			break
		}
	}
	s.removeOrphans(ctx)
	return total, nil
}

// removeOrphans deletes staging files nothing has written to for longer than
// an upload can idle. Every accepted chunk both writes the file and slides
// its row's expiry, so a file that old belongs to a row that has expired —
// or to none at all, when a process died between creating the file and the
// row, or failed to remove one after its row was swept.
func (s *Service) removeOrphans(ctx context.Context) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		slog.WarnContext(ctx, "upload staging dir unreadable", "err", err)
		return
	}
	cutoff := time.Now().Add(-TTL - time.Hour)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "stale upload file not removed", "file", e.Name(), "err", err)
		}
	}
}

// Run sweeps expired uploads every interval until ctx is cancelled.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.Sweep(ctx); err != nil {
				slog.WarnContext(ctx, "upload sweep failed", "err", err)
			} else if n > 0 {
				slog.InfoContext(ctx, "expired uploads removed", "count", n)
			}
		}
	}
}

// discard drops an upload whose staging file can no longer back its row and
// reports it as gone, which sends the client back to creating a new one.
func (s *Service) discard(ctx context.Context, up store.Upload, why string) error {
	slog.WarnContext(ctx, "discarding resumable upload", "upload", up.ID, "reason", why)
	if err := s.Terminate(ctx, up); err != nil {
		return err
	}
	return ErrNotFound
}

func expiry() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(TTL), Valid: true}
}

// stagingName is a random file name. It is not derived from the upload id so
// the file can exist before the row does.
func stagingName() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
