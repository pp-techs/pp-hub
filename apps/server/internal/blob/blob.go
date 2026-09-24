// Package blob implements the content-addressed blob store.
// See docs/architecture/storage.md.
package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zeebo/blake3"
)

var ErrNotExist = errors.New("blob does not exist")

// Store is the CAS interface. Implementations: LocalStore (MVP), S3 later.
type Store interface {
	// Put streams r into the store, returning its blake3 hash and size.
	// Content is hashed while writing; identical content dedups naturally.
	Put(ctx context.Context, r io.Reader) (hash string, size int64, err error)
	// Ingest takes ownership of a complete file already on local disk — a
	// resumable upload's staging file — and stores it under its hash. On
	// success the file no longer exists at path; on failure it is left for
	// the caller. Where the store and the file share a filesystem the bytes
	// are renamed into place, not copied.
	Ingest(ctx context.Context, path string) (hash string, size int64, err error)
	Get(ctx context.Context, hash string) (io.ReadCloser, error)
	Delete(ctx context.Context, hash string) error // GC only
	// Check reports whether the store can still accept writes. Nothing else
	// answers this: a read-only or unmounted blob root fails one upload at a
	// time, with no signal anywhere a probe can read.
	Check(ctx context.Context) error
}

// LocalStore keeps blobs at <root>/<2-hex>/<hash>.
type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{root: root}, nil
}

func (s *LocalStore) path(hash string) string {
	return filepath.Join(s.root, hash[:2], hash)
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(s.root, "tmp-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after rename

	h := blake3.New()
	size, err := io.Copy(tmp, io.TeeReader(r, h))
	if err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}

	hash := hex.EncodeToString(h.Sum(nil))
	if err := s.install(tmpName, hash); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// Ingest hashes the file in one read pass and renames it into place. The
// staging directory lives under the blob root (config.UploadDir), so the
// rename never crosses a filesystem.
func (s *LocalStore) Ingest(_ context.Context, path string) (string, int64, error) {
	hash, size, err := hashFile(path)
	if err != nil {
		return "", 0, err
	}
	if err := s.install(path, hash); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// install moves a fully written file to its content address, or removes it
// when identical content is already stored.
func (s *LocalStore) install(path, hash string) error {
	final := s.path(hash)
	if _, err := os.Stat(final); err == nil {
		return os.Remove(path) // already stored — dedup
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	return os.Rename(path, final)
}

// hashFile returns the blake3 hash and size of the file at path.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := blake3.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func (s *LocalStore) Get(_ context.Context, hash string) (io.ReadCloser, error) {
	if len(hash) < 2 {
		return nil, ErrNotExist
	}
	f, err := os.Open(s.path(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotExist
	}
	return f, err
}

func (s *LocalStore) Delete(_ context.Context, hash string) error {
	if len(hash) < 2 {
		return fmt.Errorf("invalid hash")
	}
	err := os.Remove(s.path(hash))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) Check(_ context.Context) error {
	info, err := os.Stat(s.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("blob root %s is not a directory", s.root)
	}
	// A write probe, not just a stat: the image pre-creates /data/blobs, so a
	// missing or read-only mount still stats clean.
	probe := filepath.Join(s.root, ".probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return err
	}
	return os.Remove(probe)
}
