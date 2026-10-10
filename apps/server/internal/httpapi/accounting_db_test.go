package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nhatbui/pp-hub/apps/server/internal/auth"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Retention keeps the newest versions of a file, never the current one's
// blob away from it, and hands every pruned blob back to GC — ref_count is
// what the sweep trusts, so a prune that forgot to decrement leaks the disk
// the policy exists to reclaim.
func TestVersionRetentionKeepsTheNewestAndReleasesTheRest(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	ctx := context.Background()
	sp, owner := memberSpace(t, srv, q, "retention")
	srv.fs.SetVersionRetention(func() int { return 3 })

	var file store.Node
	var hashes []string
	for i := range 5 {
		content := randomBytes(t, 100+i)
		n, ver, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, file.ID, "plan.txt", bytes.NewReader(content), owner.ID)
		if err != nil {
			t.Fatalf("version %d: %v", i+1, err)
		}
		file = n
		hashes = append(hashes, ver.BlobHash)
	}

	versions, err := q.ListVersions(ctx, file.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("keep 3: have %d versions", len(versions))
	}
	for i, want := range []int32{5, 4, 3} {
		if versions[i].VersionNo != want {
			t.Fatalf("kept versions %v, want 5,4,3", versionNos(versions))
		}
	}
	if versions[0].ID != file.CurrentVersionID {
		t.Fatal("the current version must be among those kept")
	}
	for i, h := range hashes {
		b, err := q.GetBlob(ctx, h)
		if err != nil {
			t.Fatalf("blob %d: %v", i+1, err)
		}
		want := int32(1)
		if i < 2 {
			want = 0 // versions 1 and 2 were pruned
		}
		if b.RefCount != want {
			t.Fatalf("version %d blob ref_count = %d, want %d", i+1, b.RefCount, want)
		}
	}

	// Usage follows the prune: only the three kept versions occupy the space.
	sp, err = q.GetSpace(ctx, sp.ID)
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	if want := int64(102 + 103 + 104); sp.UsedBytes != want {
		t.Fatalf("used_bytes = %d, want %d", sp.UsedBytes, want)
	}
}

// The breakdown and the meter must agree: superseded versions and trash are
// slices of their own, and the slices sum to used_bytes.
func TestUsageBreakdownAddsUpToTheMeter(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	ctx := context.Background()
	sp, owner := memberSpace(t, srv, q, "breakdown")

	doc, _, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, pgtype.UUID{}, "a.pdf", bytes.NewReader(randomBytes(t, 1000)), owner.ID)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, _, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, doc.ID, "a.pdf", bytes.NewReader(randomBytes(t, 400)), owner.ID); err != nil {
		t.Fatalf("second version: %v", err)
	}
	gone, _, err := srv.fs.PutFile(ctx, sp.ID, pgtype.UUID{}, pgtype.UUID{}, "old.png", bytes.NewReader(randomBytes(t, 70)), owner.ID)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := srv.fs.Delete(ctx, gone.ID, owner.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	rows, err := srv.fs.UsageByCategory(ctx, owner.ID, sp.ID)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	got := map[string]store.SumSpaceUsageByCategoryRow{}
	var sum int64
	for _, r := range rows {
		got[r.Category] = r
		sum += r.Bytes
	}
	if got["document"].Bytes != 400 || got["versions"].Bytes != 1000 || got["trash"].Bytes != 70 {
		t.Fatalf("breakdown %+v, want document 400, versions 1000, trash 70", got)
	}
	if got["versions"].Files != 1 || got["trash"].Files != 1 {
		t.Fatalf("counts %+v, want one superseded version and one trashed file", got)
	}
	sp, err = q.GetSpace(ctx, sp.ID)
	if err != nil {
		t.Fatalf("get space: %v", err)
	}
	if sum != sp.UsedBytes {
		t.Fatalf("breakdown sums to %d, meter says %d", sum, sp.UsedBytes)
	}
}

// used_bytes is kept by triggers, not recounted by writers: after every write,
// prune, copy and purge — a folder's cascade included — the stored figure must
// equal a fresh sum and the bytes actually written.
func TestUsedBytesFollowsWritesPrunesCopiesAndPurges(t *testing.T) {
	srv, q, _ := uploadEnv(t)
	ctx := context.Background()
	sp, owner := memberSpace(t, srv, q, "usedbytes")
	expect := func(step string, want int64) {
		t.Helper()
		got, err := q.GetSpace(ctx, sp.ID)
		if err != nil {
			t.Fatalf("%s: get space: %v", step, err)
		}
		sum, err := q.SumSpaceUsage(ctx, sp.ID)
		if err != nil {
			t.Fatalf("%s: sum: %v", step, err)
		}
		if got.UsedBytes != sum || got.UsedBytes != want {
			t.Fatalf("%s: used_bytes = %d, sum = %d, want %d", step, got.UsedBytes, sum, want)
		}
	}
	put := func(parent, node pgtype.UUID, name string, n int) store.Node {
		t.Helper()
		f, _, err := srv.fs.PutFile(ctx, sp.ID, parent, node, name, bytes.NewReader(randomBytes(t, n)), owner.ID)
		if err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
		return f
	}
	purge := func(id pgtype.UUID) {
		t.Helper()
		if err := srv.fs.Delete(ctx, id, owner.ID); err != nil {
			t.Fatalf("trash: %v", err)
		}
		if _, err := srv.fs.Purge(ctx, id); err != nil {
			t.Fatalf("purge: %v", err)
		}
	}

	file := put(pgtype.UUID{}, pgtype.UUID{}, "a.bin", 100)
	expect("first version", 100)
	put(pgtype.UUID{}, file.ID, "a.bin", 50)
	expect("second version", 150)

	srv.fs.SetVersionRetention(func() int { return 1 })
	put(pgtype.UUID{}, file.ID, "a.bin", 30)
	expect("pruned to the current version", 30)

	r := chi.NewRouter()
	r.Post("/api/v1/nodes/{nodeID}/copy", srv.handleCopyNode)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+uuidString(file.ID)+"/copy", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), sessionCtxKey, &auth.Session{ID: owner.ID, User: owner}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body.String())
	}
	expect("copy", 60)

	purge(file.ID)
	expect("purge of the original", 30)

	dir, err := srv.fs.Mkdir(ctx, sp.ID, pgtype.UUID{}, "box", owner.ID)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	put(dir.ID, pgtype.UUID{}, "inner.bin", 20)
	expect("file in folder", 50)
	purge(dir.ID)
	expect("purge of the folder", 30)
}

func versionNos(vs []store.NodeVersion) []int32 {
	out := make([]int32, len(vs))
	for i, v := range vs {
		out[i] = v.VersionNo
	}
	return out
}
