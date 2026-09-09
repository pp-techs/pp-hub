package blob

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello content-addressed world")
	hash, size, err := s.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}
	if len(hash) != 64 {
		t.Fatalf("blake3 hex hash should be 64 chars, got %d", len(hash))
	}

	rc, err := s.Get(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("round-trip content mismatch")
	}
}

func TestPutDedup(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalStore(dir)
	data := []byte("same content twice")
	h1, _, _ := s.Put(context.Background(), bytes.NewReader(data))
	h2, _, _ := s.Put(context.Background(), bytes.NewReader(data))
	if h1 != h2 {
		t.Fatal("same content must produce same hash")
	}
	// Exactly one file on disk.
	entries, _ := os.ReadDir(dir + "/" + h1[:2])
	if len(entries) != 1 {
		t.Fatalf("expected 1 blob file, got %d", len(entries))
	}
}

func TestGetMissing(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalStore(dir)
	_, err := s.Get(context.Background(), strings.Repeat("ab", 32))
	if err != ErrNotExist {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestDeleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewLocalStore(dir)
	h, _, _ := s.Put(context.Background(), strings.NewReader("x"))
	if err := s.Delete(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), h); err != nil {
		t.Fatal("delete of missing blob must be nil (GC idempotency)")
	}
}
