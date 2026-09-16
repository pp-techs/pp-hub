package blob

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// Everything every Store implementation must do.
//
// This runs against LocalStore always and S3Store when PP_TEST_S3_ENDPOINT is
// set. The point is that the two backends cannot drift: callers treat Store as
// interchangeable — fs, gc, quotas and the preview worker all hold the
// interface — so a behavioural difference between them surfaces as a bug
// somewhere far away from here.
func runStoreConformance(t *testing.T, newStore func(t *testing.T) Store) {
	t.Run("PutGetRoundTrip", func(t *testing.T) {
		s := newStore(t)
		content := []byte("hello content-addressed world")
		hash, size, err := s.Put(t.Context(), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if size != int64(len(content)) {
			t.Fatalf("size = %d, want %d", size, len(content))
		}
		if len(hash) != 64 {
			t.Fatalf("blake3 hex hash should be 64 chars, got %d", len(hash))
		}
		rc, err := s.Get(t.Context(), hash)
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
	})

	// Identical content must land on one address in both backends, or dedup
	// silently stops working and ref_count accounting drifts.
	t.Run("PutIsContentAddressed", func(t *testing.T) {
		s := newStore(t)
		data := []byte("same content twice")
		h1, _, err := s.Put(t.Context(), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		h2, n2, err := s.Put(t.Context(), bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if h1 != h2 {
			t.Fatal("same content must produce same hash")
		}
		if n2 != int64(len(data)) {
			t.Errorf("deduped Put reported size %d, want %d", n2, len(data))
		}
	})

	// GC and every read path distinguish "gone" from "broken" by this error.
	t.Run("GetMissingIsErrNotExist", func(t *testing.T) {
		s := newStore(t)
		_, err := s.Get(t.Context(), strings.Repeat("ab", 32))
		if err != ErrNotExist {
			t.Fatalf("want ErrNotExist, got %v", err)
		}
	})

	// The GC sweep can race itself and another delete; both must finish.
	t.Run("DeleteIsIdempotent", func(t *testing.T) {
		s := newStore(t)
		h, _, err := s.Put(t.Context(), strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(t.Context(), h); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(t.Context(), h); err != nil {
			t.Fatal("delete of missing blob must be nil (GC idempotency)")
		}
		if _, err := s.Get(t.Context(), h); err != ErrNotExist {
			t.Fatalf("after delete, Get should be ErrNotExist, got %v", err)
		}
	})

	t.Run("EmptyBlobRoundTrips", func(t *testing.T) {
		s := newStore(t)
		hash, size, err := s.Put(t.Context(), bytes.NewReader(nil))
		if err != nil {
			t.Fatal(err)
		}
		if size != 0 {
			t.Errorf("size = %d, want 0", size)
		}
		rc, err := s.Get(t.Context(), hash)
		if err != nil {
			t.Fatalf("empty blob must be retrievable: %v", err)
		}
		defer rc.Close()
		got, _ := io.ReadAll(rc)
		if len(got) != 0 {
			t.Errorf("got %d bytes, want 0", len(got))
		}
	})

	// A short or malformed hash must be rejected rather than reaching the
	// backend — hash[:2] would panic on a 1-character string.
	t.Run("ShortHashIsRejected", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Get(t.Context(), "a"); err == nil {
			t.Error("Get with a 1-char hash should error, not panic")
		}
		if err := s.Delete(t.Context(), "a"); err == nil {
			t.Error("Delete with a 1-char hash should error, not panic")
		}
	})

	t.Run("LargerThanOneBufferRoundTrips", func(t *testing.T) {
		s := newStore(t)
		content := bytes.Repeat([]byte("pp-hub"), 200_000) // ~1.2MB
		hash, size, err := s.Put(t.Context(), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if size != int64(len(content)) {
			t.Fatalf("size = %d, want %d", size, len(content))
		}
		rc, err := s.Get(t.Context(), hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		got, _ := io.ReadAll(rc)
		if !bytes.Equal(got, content) {
			t.Fatal("large round-trip mismatch")
		}
	})
}

func TestLocalStoreConformance(t *testing.T) {
	runStoreConformance(t, func(t *testing.T) Store {
		s, err := NewLocalStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

// TestS3StoreConformance needs an S3-compatible endpoint:
//
//	docker run -p 9000:9000 minio/minio server /data
//	PP_TEST_S3_ENDPOINT=http://localhost:9000 \
//	PP_TEST_S3_BUCKET=pphub-test \
//	PP_TEST_S3_ACCESS_KEY_ID=minioadmin \
//	PP_TEST_S3_SECRET_ACCESS_KEY=minioadmin \
//	go test ./internal/blob/
func TestS3StoreConformance(t *testing.T) {
	endpoint := os.Getenv("PP_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("PP_TEST_S3_ENDPOINT not set; skipping S3 conformance")
	}
	runStoreConformance(t, func(t *testing.T) Store {
		s, err := NewS3Store(context.Background(), S3Config{
			Bucket:    os.Getenv("PP_TEST_S3_BUCKET"),
			Endpoint:  endpoint,
			AccessKey: os.Getenv("PP_TEST_S3_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("PP_TEST_S3_SECRET_ACCESS_KEY"),
			PathStyle: true,
			Spool:     t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestNewS3StoreRequiresBucket(t *testing.T) {
	if _, err := NewS3Store(context.Background(), S3Config{}); err == nil {
		t.Error("NewS3Store with no bucket must fail rather than start half-configured")
	}
}
