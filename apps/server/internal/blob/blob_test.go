package blob

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func newLocal(t *testing.T) (*LocalStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewLocalStore(dir, &fakeClaims{})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func readBlob(t *testing.T, s Store, hash string) []byte {
	t.Helper()
	rc, err := s.Get(t.Context(), hash)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Dedup means one file on disk, and the second write's temp file gone.
func TestLocalPutDedupLeavesOneFile(t *testing.T) {
	s, dir := newLocal(t)
	data := []byte("same content twice")
	h, _, _ := s.Put(t.Context(), bytes.NewReader(data))
	if _, _, err := s.Put(t.Context(), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, h[:2]))
	if len(entries) != 1 {
		t.Fatalf("expected 1 blob file, got %d", len(entries))
	}
	root, _ := os.ReadDir(dir)
	for _, e := range root {
		if !e.IsDir() {
			t.Fatalf("stray file %q at the blob root", e.Name())
		}
	}
}

// A copy with the right name and the wrong length is damage — a write torn by
// a crash — and must not be what later uploads of that content deduplicate
// against.
func TestLocalPutReplacesATruncatedCopy(t *testing.T) {
	s, dir := newLocal(t)
	content := bytes.Repeat([]byte("intact "), 1000)
	hash, _, err := s.Put(t.Context(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(dir, hash[:2], hash), 10); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Put(t.Context(), bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	if got := readBlob(t, s, hash); !bytes.Equal(got, content) {
		t.Fatalf("stored copy still damaged: %d bytes", len(got))
	}
}

// Same length, wrong bytes: only the scrub can tell, and once it has marked
// the row the next write must overwrite the copy.
func TestLocalPutOverwritesACopyMarkedDamaged(t *testing.T) {
	claims := &fakeClaims{damaged: map[string]bool{}}
	dir := t.TempDir()
	s, err := NewLocalStore(dir, claims)
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("abcdefgh"), 512)
	hash, _, err := s.Put(t.Context(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	rotten := bytes.Repeat([]byte("X"), len(content))
	if err := os.WriteFile(filepath.Join(dir, hash[:2], hash), rotten, 0o600); err != nil {
		t.Fatal(err)
	}
	claims.damaged[hash] = true
	if _, _, err := s.Put(t.Context(), bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	if got := readBlob(t, s, hash); !bytes.Equal(got, content) {
		t.Fatal("a copy marked damaged was deduplicated against instead of replaced")
	}
}

// Walk reports blobs only: not the upload staging area, temp files, the
// readiness probe, or anything filed under the wrong prefix.
func TestLocalWalkSkipsWhatIsNotABlob(t *testing.T) {
	s, dir := newLocal(t)
	hash, _, err := s.Put(t.Context(), bytes.NewReader([]byte("a blob")))
	if err != nil {
		t.Fatal(err)
	}
	misfiled := "ff" + hash[2:]
	if hash[:2] == "ff" {
		misfiled = "00" + hash[2:]
	}
	for _, p := range []string{
		filepath.Join(dir, ".uploads", "0123"),
		filepath.Join(dir, tempPrefix+"123"),
		filepath.Join(dir, ".probe"),
		filepath.Join(dir, hash[:2], "not-a-hash"),
		filepath.Join(dir, hash[:2], misfiled),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := s.Walk(t.Context(), func(h string, _ int64) error {
		got = append(got, h)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("Walk = %v, want [%s]", got, hash)
	}
}

// Tidy collects temp files a crashed write abandoned, and only those old
// enough that no write can still be using them.
func TestLocalTidyRemovesOnlyStaleTempFiles(t *testing.T) {
	s, dir := newLocal(t)
	hash, _, err := s.Put(t.Context(), bytes.NewReader([]byte("keep me")))
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, tempPrefix+"stale")
	fresh := filepath.Join(dir, tempPrefix+"fresh")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	n, err := s.Tidy(t.Context(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Tidy removed %d, want 1", n)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale temp file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a temp file young enough to be in use was removed")
	}
	if got := readBlob(t, s, hash); string(got) != "keep me" {
		t.Error("Tidy touched a blob")
	}
}
