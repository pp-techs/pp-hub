// Package blob implements the content-addressed blob store.
// See docs/architecture/storage.md.
package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zeebo/blake3"
)

var ErrNotExist = errors.New("blob does not exist")

// Claims is the metadata side of a write. A store calls Claim once it knows
// the hash and before it installs or deduplicates the bytes, so that from
// that moment GC cannot delete what the caller is about to reference. It is
// the reason no caller has to order "write bytes" against "record them".
type Claims interface {
	// Claim records hash and pins it against collection. replace reports that
	// the stored copy is known damaged and must be overwritten, not reused.
	Claim(ctx context.Context, hash string, size int64) (replace bool, err error)
	// Healed records that a damaged copy has been replaced with good bytes.
	Healed(ctx context.Context, hash string) error
}

// Store is the CAS interface. Implementations: LocalStore and S3Store.
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
	// Walk calls fn with the hash and size of every stored blob, in no
	// particular order. The storage audit uses it to find bytes no row
	// accounts for.
	Walk(ctx context.Context, fn func(hash string, size int64) error) error
	// Tidy removes temporary files older than age — what a write that died
	// between creating its temp file and installing it leaves behind.
	Tidy(ctx context.Context, age time.Duration) (removed int, err error)
}

// tempPrefix names LocalStore's in-flight files at the blob root. Walk skips
// them and Tidy collects them.
const tempPrefix = "tmp-"

// LocalStore keeps blobs at <root>/<2-hex>/<hash>.
type LocalStore struct {
	root   string
	claims Claims
}

func NewLocalStore(root string, claims Claims) (*LocalStore, error) {
	if claims == nil {
		return nil, errors.New("blob: a store needs claims to record its writes")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalStore{root: root, claims: claims}, nil
}

func (s *LocalStore) path(hash string) string {
	return filepath.Join(s.root, hash[:2], hash)
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(s.root, tempPrefix+"*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after rename

	h := blake3.New()
	size, err := io.Copy(tmp, io.TeeReader(r, h))
	if err == nil {
		// Durable before it gets a name. The reference committed after this
		// returns is durable in Postgres; bytes still in the page cache would
		// let a power cut leave that reference pointing at an empty file.
		err = tmp.Sync()
	}
	if err != nil {
		_ = tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}

	hash := hex.EncodeToString(h.Sum(nil))
	if err := s.install(ctx, tmpName, hash, size); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// Ingest hashes the file in one read pass and renames it into place. The
// staging directory lives under the blob root (config.UploadDir), so the
// rename normally never crosses a filesystem; see moveFile for when it does.
func (s *LocalStore) Ingest(ctx context.Context, path string) (string, int64, error) {
	hash, size, err := hashFile(path)
	if err != nil {
		return "", 0, err
	}
	// The staging file is fsynced chunk by chunk as it arrives; this is a
	// no-op then, and the guarantee if anything else ever calls Ingest.
	if err := syncFile(path); err != nil {
		return "", 0, err
	}
	if err := s.install(ctx, path, hash, size); err != nil {
		return "", 0, err
	}
	return hash, size, nil
}

// install claims hash, then moves a fully written file to its content
// address — or removes it when an intact copy is already stored.
//
// The claim comes first on purpose. A stored copy found here may belong to a
// blob GC is about to collect; claiming pins it, so the copy this write
// deduplicates against is still there when the caller references it.
func (s *LocalStore) install(ctx context.Context, path, hash string, size int64) error {
	replace, err := s.claims.Claim(ctx, hash, size)
	if err != nil {
		return fmt.Errorf("claim blob: %w", err)
	}
	final := s.path(hash)
	if !replace {
		info, err := os.Stat(final)
		switch {
		case err == nil && info.Size() == size:
			return os.Remove(path) // already stored — dedup
		case err == nil:
			// Same name, different length: the stored copy is damaged (a
			// write torn by a crash, say). Keeping it would make every
			// future upload of this content inherit the damage.
			slog.WarnContext(ctx, "blob: stored copy has the wrong size, replacing it",
				"hash", short(hash), "stored", info.Size(), "size", size)
			replace = true
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
	}
	dir := filepath.Dir(final)
	if err := s.mkShard(dir); err != nil {
		return err
	}
	if err := s.moveFile(path, final); err != nil {
		return err
	}
	// The rename is only durable once the directory entry is.
	if err := syncDir(dir); err != nil {
		return err
	}
	if replace {
		if err := s.claims.Healed(ctx, hash); err != nil {
			// The bytes are good; the scrub clears the mark on its next pass.
			slog.WarnContext(ctx, "blob: healed copy not recorded", "hash", short(hash), "err", err)
		}
	}
	return nil
}

// mkShard creates a <2-hex> directory durably: a new directory entry in the
// root is as easy to lose in a crash as the file that is about to go in it.
func (s *LocalStore) mkShard(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return syncDir(s.root)
}

// moveFile renames src to dst. A staging directory an operator has mounted
// on a different filesystem cannot be renamed out of; then the bytes are
// copied to a temp file in the store first, made durable, and renamed from
// there, so the content address still never shows a partial file.
func (s *LocalStore) moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(s.root, tempPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	_, err = io.Copy(tmp, in)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	return os.Remove(src)
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

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// syncDir makes a directory's entries durable. A filesystem that cannot
// fsync a directory says EINVAL; there is nothing more to do on one.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
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

// Walk visits <root>/<2-hex>/<hash>, one shard directory at a time, so memory
// stays bounded by the largest shard however many blobs there are. Anything
// that is not a blob under its own prefix — staging, temp files, the probe —
// is skipped.
func (s *LocalStore) Walk(ctx context.Context, fn func(hash string, size int64) error) error {
	shards, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !shard.IsDir() || len(shard.Name()) != 2 || !isHex(shard.Name()) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(s.root, shard.Name()))
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !IsHash(name) || name[:2] != shard.Name() {
				continue
			}
			info, err := e.Info()
			if errors.Is(err, os.ErrNotExist) {
				continue // collected since the listing
			}
			if err != nil {
				return err
			}
			if err := fn(name, info.Size()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *LocalStore) Tidy(_ context.Context, age time.Duration) (int, error) {
	return removeStale(s.root, tempPrefix, age)
}

// removeStale deletes files in dir named prefix* last modified before age
// ago. The age is what keeps it from taking a write that is still running.
func removeStale(dir, prefix string, age time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-age)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) < len(prefix) || e.Name()[:len(prefix)] != prefix {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
			removed++
		} else if !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
	}
	return removed, nil
}

// IsHash reports whether s is a blob name: 64 lowercase hex characters, the
// encoding of a blake3-256 digest.
func IsHash(s string) bool {
	return len(s) == 64 && isHex(s)
}

// Hash is the name Put and Ingest give p: its hex blake3-256 digest. It lets
// a caller holding bytes ask whether they are already the ones stored under a
// hash without reading the stored copy back.
func Hash(p []byte) string {
	sum := blake3.Sum256(p)
	return hex.EncodeToString(sum[:])
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}
