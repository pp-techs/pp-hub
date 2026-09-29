package gc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zeebo/blake3"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Reference counting and collection live in SQL — triggers, row locks, the
// sweep's guards — so these run against a real Postgres or skip loudly:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/gc/
//
// They never call Sweep, Scrub or the listings: those see every row in the
// database, and a developer's database has rows of its own. They drive the
// per-blob steps (collect, check) on hashes they created.
type env struct {
	pool  *pgxpool.Pool
	q     *store.Queries
	user  store.User
	space store.Space
	dir   string
	blobs blob.Store
	fs    *fs.Service
	gc    *Worker
}

func newEnv(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("PP_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("PP_TEST_DATABASE_URL not set; skipping database-backed test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	q := store.New(pool)
	sub := fmt.Sprintf("test-gc-%d-%d", os.Getpid(), time.Now().UnixNano())
	u, err := q.CreateUser(ctx, store.CreateUserParams{OidcSub: sub, OidcIss: "test", Email: sub + "@test.local", Name: "gc"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	sp, err := q.CreateSpace(ctx, store.CreateSpaceParams{Name: sub, OwnerID: u.ID})
	if err != nil {
		t.Fatalf("space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	dir := t.TempDir()
	blobs, err := blob.NewLocalStore(dir, q.BlobClaims())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	return &env{
		pool: pool, q: q, user: u, space: sp, dir: dir, blobs: blobs,
		fs: fs.NewService(q, blobs), gc: NewWorker(q, blobs, 0),
	}
}

func unique(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 2048)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func hashOf(b []byte) string {
	sum := blake3.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (e *env) put(t *testing.T, name string, content []byte) (store.Node, store.NodeVersion) {
	t.Helper()
	n, v, err := e.fs.PutFile(t.Context(), e.space.ID, pgtype.UUID{}, pgtype.UUID{}, name, bytes.NewReader(content), e.user.ID)
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return n, v
}

func (e *env) refs(t *testing.T, hash string) int32 {
	t.Helper()
	b, err := e.q.GetBlob(t.Context(), hash)
	if err != nil {
		t.Fatalf("blob row %s: %v", hash[:12], err)
	}
	return b.RefCount
}

// lapse ages a row past the GC grace period and any claim, as if it had been
// unreferenced and untouched for hours.
func (e *env) lapse(t *testing.T, hash string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(),
		`UPDATE blobs SET released_at = now() - interval '2 hours', pinned_until = now() - interval '1 second' WHERE hash = $1`, hash); err != nil {
		t.Fatal(err)
	}
}

func (e *env) onDisk(hash string) bool {
	_, err := os.Stat(filepath.Join(e.dir, hash[:2], hash))
	return err == nil
}

// collectingStore runs a collection of the blob it just stored, in the window
// between the store deduplicating the bytes and the caller referencing them.
type collectingStore struct {
	blob.Store
	collect func(hash string)
}

func (s *collectingStore) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	h, n, err := s.Store.Put(ctx, r)
	if err == nil {
		s.collect(h)
	}
	return h, n, err
}

// The bug this guards: a file deleted long ago releases its blob; the same
// content is uploaded again; the store finds the old copy and deduplicates;
// GC collects that copy before the new version references it. The upload
// then points at nothing, silently. The store's claim must pin the blob.
func TestCollectSparesABlobAnUploadJustDeduplicatedAgainst(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	content := unique(t)
	hash := hashOf(content)

	old, _ := e.put(t, "report.txt", content)
	if err := e.fs.Delete(ctx, old.ID, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.fs.Purge(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hash); n != 0 {
		t.Fatalf("after purge ref_count = %d, want 0", n)
	}
	e.lapse(t, hash)

	var collectErr error
	racing := &collectingStore{Store: e.blobs, collect: func(h string) {
		_, collectErr = e.gc.collect(ctx, h)
	}}
	node, _, err := fs.NewService(e.q, racing).PutFile(ctx, e.space.ID, pgtype.UUID{}, pgtype.UUID{}, "report.txt", bytes.NewReader(content), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(collectErr, errKept) {
		t.Fatalf("GC in the dedup window: %v, want the blob kept", collectErr)
	}
	_, _, rc, err := e.fs.OpenFile(ctx, node.ID)
	if err != nil {
		t.Fatalf("the re-uploaded file is unreadable: %v", err)
	}
	defer rc.Close()
	if got, _ := io.ReadAll(rc); !bytes.Equal(got, content) {
		t.Fatal("the re-uploaded file does not hold its content")
	}
}

// A write refused after its bytes landed (here: quota) must not leak them.
// The claim gave the bytes a row, so once it lapses the sweep takes them.
func TestRefusedUploadIsCollectedOnceItsClaimLapses(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	if _, err := e.pool.Exec(ctx, `UPDATE spaces SET quota_bytes = 1 WHERE id = $1`, e.space.ID); err != nil {
		t.Fatal(err)
	}
	content := unique(t)
	hash := hashOf(content)
	if _, _, err := e.fs.PutFile(ctx, e.space.ID, pgtype.UUID{}, pgtype.UUID{}, "big.bin", bytes.NewReader(content), e.user.ID); !errors.Is(err, fs.ErrQuotaExceeded) {
		t.Fatalf("upload over quota: %v", err)
	}
	if n := e.refs(t, hash); n != 0 {
		t.Fatalf("refused upload holds %d references", n)
	}
	if _, err := e.gc.collect(ctx, hash); !errors.Is(err, errKept) {
		t.Fatalf("a claimed blob was collected before its claim lapsed: %v", err)
	}
	e.lapse(t, hash)
	if _, err := e.gc.collect(ctx, hash); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if e.onDisk(hash) {
		t.Fatal("the refused upload's bytes are still on disk")
	}
	if _, err := e.q.GetBlob(ctx, hash); err == nil {
		t.Fatal("the refused upload's row survived collection")
	}
}

// ref_count is the number of rows referencing the blob — versions, previews,
// avatars — whatever removed them, cascades included.
func TestRefCountFollowsTheReferencingRows(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	c, d := unique(t), unique(t)
	hc, hd := hashOf(c), hashOf(d)

	a, _ := e.put(t, "a.txt", c)
	if _, err := e.fs.Copy(ctx, a.ID, pgtype.UUID{}, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 2 {
		t.Fatalf("file + copy: ref_count = %d, want 2", n)
	}

	// A new version of a with retention 1 prunes the old one.
	e.fs.SetVersionRetention(func() int { return 1 })
	_, v2, err := e.fs.PutFile(ctx, e.space.ID, pgtype.UUID{}, a.ID, "a.txt", bytes.NewReader(d), e.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 1 {
		t.Fatalf("after pruning a's first version: ref_count(c) = %d, want 1", n)
	}

	if err := e.q.UpsertPreview(ctx, store.UpsertPreviewParams{NodeVersionID: v2.ID, Kind: "thumb", BlobHash: hd}); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hd); n != 2 {
		t.Fatalf("version + preview: ref_count(d) = %d, want 2", n)
	}
	if err := e.q.UpsertPreview(ctx, store.UpsertPreviewParams{NodeVersionID: v2.ID, Kind: "thumb", BlobHash: hc}); err != nil {
		t.Fatal(err)
	}
	if n1, n2 := e.refs(t, hd), e.refs(t, hc); n1 != 1 || n2 != 2 {
		t.Fatalf("preview moved d→c: ref_count(d)=%d ref_count(c)=%d, want 1 and 2", n1, n2)
	}

	if _, err := e.q.SetUserAvatar(ctx, store.SetUserAvatarParams{ID: e.user.ID, AvatarBlob: hc, PrevBlob: ""}); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 3 {
		t.Fatalf("copy + preview + avatar: ref_count(c) = %d, want 3", n)
	}

	before := time.Now()
	if err := e.fs.Delete(ctx, a.ID, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.fs.Purge(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if n1, n2 := e.refs(t, hd), e.refs(t, hc); n1 != 0 || n2 != 2 {
		t.Fatalf("after purging a: ref_count(d)=%d ref_count(c)=%d, want 0 and 2", n1, n2)
	}
	b, _ := e.q.GetBlob(ctx, hd)
	if b.ReleasedAt.Time.Before(before.Add(-time.Second)) {
		t.Fatalf("released_at = %v, not moved when d lost its last reference", b.ReleasedAt.Time)
	}

	if _, err := e.q.SetUserAvatar(ctx, store.SetUserAvatarParams{ID: e.user.ID, AvatarBlob: "", PrevBlob: hc}); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 1 {
		t.Fatalf("avatar cleared: ref_count(c) = %d, want 1", n)
	}

	// A space's avatar is a reference the same way, on its own trigger.
	if _, err := e.q.SetSpaceAvatar(ctx, store.SetSpaceAvatarParams{ID: e.space.ID, AvatarBlob: hc, PrevBlob: ""}); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 2 {
		t.Fatalf("space avatar set: ref_count(c) = %d, want 2", n)
	}
	if _, err := e.q.SetSpaceAvatar(ctx, store.SetSpaceAvatarParams{ID: e.space.ID, AvatarBlob: "", PrevBlob: hc}); err != nil {
		t.Fatal(err)
	}
	if n := e.refs(t, hc); n != 1 {
		t.Fatalf("space avatar cleared: ref_count(c) = %d, want 1", n)
	}
}

// Bytes in the store with no row — what a crash between writing and
// recording used to leave, and every preview before previews were counted —
// are adopted with their true reference count, and the unreferenced ones then
// collected by the ordinary sweep.
func TestAuditAdoptsBytesWithoutARow(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	_, v := e.put(t, "a.txt", unique(t))

	writeRaw := func(content []byte) string {
		h := hashOf(content)
		p := filepath.Join(e.dir, h[:2], h)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return h
	}
	previewed := writeRaw(unique(t))
	orphan := writeRaw(unique(t))
	if err := e.q.UpsertPreview(ctx, store.UpsertPreviewParams{NodeVersionID: v.ID, Kind: "thumb", BlobHash: previewed}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.GetBlob(ctx, previewed); err == nil {
		t.Fatal("setup: the previewed blob already has a row")
	}

	rep, err := e.gc.Audit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Adopted < 2 {
		t.Fatalf("adopted %d, want at least 2", rep.Adopted)
	}
	if n := e.refs(t, previewed); n != 1 {
		t.Fatalf("adopted preview blob: ref_count = %d, want 1", n)
	}
	if n := e.refs(t, orphan); n != 0 {
		t.Fatalf("adopted orphan: ref_count = %d, want 0", n)
	}
	e.lapse(t, orphan)
	e.lapse(t, previewed)
	if _, err := e.gc.collect(ctx, previewed); !errors.Is(err, errKept) {
		t.Fatalf("a preview's blob was collectable: %v", err)
	}
	if _, err := e.gc.collect(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	if e.onDisk(orphan) {
		t.Fatal("the adopted orphan was not collected")
	}
}

// The scrub records damage it finds; the next write of the same content
// replaces the copy rather than deduplicating against it, and clears the mark.
func TestScrubMarksDamageAndTheNextWriteHeals(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	content := unique(t)
	hash := hashOf(content)
	// Leaves no damaged row behind to trip a developer's pphub_blobs_damaged.
	t.Cleanup(func() { _, _ = e.pool.Exec(context.Background(), `DELETE FROM blobs WHERE hash = $1`, hash) })
	e.put(t, "a.txt", content)

	if v, err := e.gc.check(ctx, hash, int64(len(content))); err != nil || v == nil || !*v {
		t.Fatalf("intact blob: verdict %v, err %v", v, err)
	}
	rotten := bytes.Repeat([]byte{0}, len(content))
	if err := os.WriteFile(filepath.Join(e.dir, hash[:2], hash), rotten, 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err := e.gc.check(ctx, hash, int64(len(content))); err != nil || v == nil || *v {
		t.Fatalf("rotten blob: verdict %v, err %v", v, err)
	}
	b, _ := e.q.GetBlob(ctx, hash)
	if !b.DamagedAt.Valid {
		t.Fatal("damage not recorded")
	}

	node, _ := e.put(t, "b.txt", content)
	_, _, rc, err := e.fs.OpenFile(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if got, _ := io.ReadAll(rc); !bytes.Equal(got, content) {
		t.Fatal("the new upload deduplicated against the damaged copy")
	}
	b, _ = e.q.GetBlob(ctx, hash)
	if b.DamagedAt.Valid {
		t.Fatal("the heal was not recorded")
	}

	if err := os.Remove(filepath.Join(e.dir, hash[:2], hash)); err != nil {
		t.Fatal(err)
	}
	if v, err := e.gc.check(ctx, hash, int64(len(content))); err != nil || v == nil || *v {
		t.Fatalf("missing blob: verdict %v, err %v", v, err)
	}
}
