package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeebo/blake3"
)

// fakeClaims stands in for the blobs table. onClaim, when set, runs inside
// Claim — which is where a test can look at the store as the metadata sees it.
type fakeClaims struct {
	mu      sync.Mutex
	fail    error
	damaged map[string]bool
	healed  []string
	onClaim func(hash string)
}

func (c *fakeClaims) Claim(_ context.Context, hash string, _ int64) (bool, error) {
	c.mu.Lock()
	fail, damaged, hook := c.fail, c.damaged[hash], c.onClaim
	c.mu.Unlock()
	if hook != nil {
		hook(hash)
	}
	return damaged, fail
}

func (c *fakeClaims) Healed(_ context.Context, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healed = append(c.healed, hash)
	delete(c.damaged, hash)
	return nil
}

// randomContent is unique per call, so tests against a shared S3 bucket never
// find a previous run's object at the address.
func randomContent(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// Everything every Store implementation must do.
//
// This runs against LocalStore always and S3Store when PP_TEST_S3_ENDPOINT is
// set. The point is that the two backends cannot drift: callers treat Store as
// interchangeable — fs, gc, quotas and the preview worker all hold the
// interface — so a behavioural difference between them surfaces as a bug
// somewhere far away from here.
func runStoreConformance(t *testing.T, newStoreWith func(t *testing.T, claims Claims) Store) {
	newStore := func(t *testing.T) Store { return newStoreWith(t, &fakeClaims{}) }

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

	// A finished resumable upload arrives as a file. Ingest must address it
	// exactly as Put would the same bytes — otherwise an uploaded file and a
	// WOPI save of identical content stop deduplicating — and must take the
	// file away, dedup or not, or the staging directory fills.
	t.Run("IngestMatchesPutAndConsumesTheFile", func(t *testing.T) {
		s := newStore(t)
		ingest := func(content []byte) string {
			t.Helper()
			staged := filepath.Join(t.TempDir(), "upload")
			if err := os.WriteFile(staged, content, 0o600); err != nil {
				t.Fatal(err)
			}
			hash, size, err := s.Ingest(t.Context(), staged)
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if size != int64(len(content)) {
				t.Fatalf("size %d, want %d", size, len(content))
			}
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Fatal("the staged file must be gone after Ingest")
			}
			rc, err := s.Get(t.Context(), hash)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer rc.Close()
			if got, _ := io.ReadAll(rc); !bytes.Equal(got, content) {
				t.Fatal("stored bytes differ from the staged file")
			}
			return hash
		}

		ingest(bytes.Repeat([]byte("fresh"), 50_000))

		content := bytes.Repeat([]byte("staged"), 50_000)
		putHash, _, err := s.Put(t.Context(), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if hash := ingest(content); hash != putHash {
			t.Fatalf("Ingest addressed identical bytes as %s, Put as %s", hash, putHash)
		}
	})

	// The claim is what pins a blob against GC while a write deduplicates
	// against it, so it must happen before the bytes are installed — a claim
	// made afterwards leaves the window the GC race used.
	t.Run("ClaimComesBeforeTheBytes", func(t *testing.T) {
		claims := &fakeClaims{}
		s := newStoreWith(t, claims)
		var visibleAtClaim error
		claims.onClaim = func(hash string) {
			rc, err := s.Get(t.Context(), hash)
			if err == nil {
				rc.Close()
			}
			visibleAtClaim = err
		}
		if _, _, err := s.Put(t.Context(), bytes.NewReader(randomContent(t, 4096))); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(visibleAtClaim, ErrNotExist) {
			t.Fatalf("the bytes were readable before they were claimed (Get at claim time: %v)", visibleAtClaim)
		}
	})

	// A write the metadata refused must leave nothing at the content address,
	// and Ingest must leave the staged file for the caller.
	t.Run("FailedClaimInstallsNothing", func(t *testing.T) {
		claims := &fakeClaims{fail: errors.New("database down")}
		s := newStoreWith(t, claims)
		content := randomContent(t, 4096)
		if _, _, err := s.Put(t.Context(), bytes.NewReader(content)); err == nil {
			t.Fatal("Put succeeded although the claim failed")
		}
		staged := filepath.Join(t.TempDir(), "upload")
		if err := os.WriteFile(staged, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Ingest(t.Context(), staged); err == nil {
			t.Fatal("Ingest succeeded although the claim failed")
		}
		if _, err := os.Stat(staged); err != nil {
			t.Fatalf("a failed Ingest must leave the staged file: %v", err)
		}
		sum := blake3.Sum256(content)
		if _, err := s.Get(t.Context(), hex.EncodeToString(sum[:])); !errors.Is(err, ErrNotExist) {
			t.Fatalf("bytes were installed for a refused claim: %v", err)
		}
	})

	// A copy the scrub marked damaged must be overwritten by the next write of
	// the same content, not deduplicated against, and the heal recorded.
	t.Run("DamagedCopyIsReplacedAndHealed", func(t *testing.T) {
		claims := &fakeClaims{damaged: map[string]bool{}}
		s := newStoreWith(t, claims)
		content := randomContent(t, 4096)
		hash, _, err := s.Put(t.Context(), bytes.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		if len(claims.healed) != 0 {
			t.Fatalf("a first write healed %v", claims.healed)
		}
		claims.damaged[hash] = true
		if _, _, err := s.Put(t.Context(), bytes.NewReader(content)); err != nil {
			t.Fatal(err)
		}
		if len(claims.healed) != 1 || claims.healed[0] != hash {
			t.Fatalf("healed = %v, want [%s]", claims.healed, hash)
		}
		rc, err := s.Get(t.Context(), hash)
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		if got, _ := io.ReadAll(rc); !bytes.Equal(got, content) {
			t.Fatal("the replaced copy does not hold the content")
		}
	})

	// The audit adopts what Walk reports, so every stored blob must be in it
	// with its real size.
	t.Run("WalkListsStoredBlobs", func(t *testing.T) {
		s := newStore(t)
		want := map[string]int64{}
		for _, n := range []int{0, 17, 70_000} {
			hash, size, err := s.Put(t.Context(), bytes.NewReader(randomContent(t, n)))
			if err != nil {
				t.Fatal(err)
			}
			want[hash] = size
		}
		err := s.Walk(t.Context(), func(hash string, size int64) error {
			if w, ok := want[hash]; ok {
				if size != w {
					t.Errorf("Walk reported %s as %d bytes, want %d", hash[:12], size, w)
				}
				delete(want, hash)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(want) != 0 {
			t.Fatalf("Walk missed %d stored blobs", len(want))
		}
	})

	// /readyz calls this on every request cycle, so a healthy store must
	// answer cleanly and cheaply on both backends.
	t.Run("CheckPassesOnAHealthyStore", func(t *testing.T) {
		s := newStore(t)
		if err := s.Check(t.Context()); err != nil {
			t.Fatalf("Check on a healthy store: %v", err)
		}
	})
}

func TestLocalStoreConformance(t *testing.T) {
	runStoreConformance(t, func(t *testing.T, claims Claims) Store {
		s, err := NewLocalStore(t.TempDir(), claims)
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
	runStoreConformance(t, func(t *testing.T, claims Claims) Store {
		s, err := NewS3Store(context.Background(), S3Config{
			Bucket:    os.Getenv("PP_TEST_S3_BUCKET"),
			Endpoint:  endpoint,
			AccessKey: os.Getenv("PP_TEST_S3_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("PP_TEST_S3_SECRET_ACCESS_KEY"),
			PathStyle: true,
			Spool:     t.TempDir(),
		}, claims)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestNewS3StoreRequiresBucket(t *testing.T) {
	if _, err := NewS3Store(context.Background(), S3Config{}, &fakeClaims{}); err == nil {
		t.Error("NewS3Store with no bucket must fail rather than start half-configured")
	}
}
