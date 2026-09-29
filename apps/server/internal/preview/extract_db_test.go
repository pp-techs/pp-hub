package preview

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/fs"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// extractEnv is a space, an fs wired to the worker the way cmd/server wires
// it, and a worker that runs extraction only.
type extractEnv struct {
	q     *store.Queries
	pool  *pgxpool.Pool
	fs    *fs.Service
	w     *Worker
	user  store.User
	space store.Space
}

func newExtractEnv(t *testing.T) *extractEnv {
	t.Helper()
	q, pool := testQueries(t)
	ctx := context.Background()
	sub := fmt.Sprintf("test-extract-%d-%d", os.Getpid(), time.Now().UnixNano())
	u, err := q.CreateUser(ctx, store.CreateUserParams{OidcSub: sub, OidcIss: "test", Email: sub + "@test.local", Name: "extract"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	sp, err := q.CreateSpace(ctx, store.CreateSpaceParams{Name: sub, OwnerID: u.ID})
	if err != nil {
		t.Fatalf("space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	blobs, err := blob.NewLocalStore(t.TempDir(), q.BlobClaims())
	if err != nil {
		t.Fatalf("blob store: %v", err)
	}
	w := NewWorker(q, blobs).WithExtractors(TextExtractor{})
	fsSvc := fs.NewService(q, blobs)
	fsSvc.SetVersionHook(func(ctx context.Context, ver store.NodeVersion, name string) {
		if err := w.Enqueue(ctx, ver, name); err != nil {
			t.Errorf("enqueue: %v", err)
		}
	})
	fsSvc.SetRestoreHook(func(ctx context.Context, ver store.NodeVersion) {
		if err := w.Reindex(ctx, ver); err != nil {
			t.Errorf("reindex: %v", err)
		}
	})
	return &extractEnv{q: q, pool: pool, fs: fsSvc, w: w, user: u, space: sp}
}

func (e *extractEnv) put(t *testing.T, fileID pgtype.UUID, name, body string) (store.Node, store.NodeVersion) {
	t.Helper()
	n, v, err := e.fs.PutFile(context.Background(), e.space.ID, pgtype.UUID{}, fileID, name, strings.NewReader(body), e.user.ID)
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return n, v
}

// drainAll runs the claim loop until no extraction job is left pending.
func (e *extractEnv) drainAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for range 50 {
		var pending int
		if err := e.pool.QueryRow(ctx,
			`SELECT count(*) FROM jobs WHERE kind = $1 AND status = 'pending' AND run_at <= now()`,
			KindExtractText).Scan(&pending); err != nil {
			t.Fatalf("count pending: %v", err)
		}
		if pending == 0 {
			return
		}
		e.w.drain(ctx)
	}
	t.Fatal("extraction jobs never drained")
}

// indexed returns the stored row for a node, or ok=false.
func (e *extractEnv) indexed(t *testing.T, nodeID pgtype.UUID) (versionID pgtype.UUID, content string, ok bool) {
	t.Helper()
	err := e.pool.QueryRow(context.Background(),
		`SELECT node_version_id, content FROM node_texts WHERE node_id = $1`, nodeID).Scan(&versionID, &content)
	if err != nil {
		return pgtype.UUID{}, "", false
	}
	return versionID, content, true
}

// The index follows the node's current version through every way it can
// change: a new version replaces the text, a late job for a superseded
// version cannot put the old text back, restoring an older version
// re-extracts it, and purging the node drops the row.
func TestExtractionFollowsTheCurrentVersion(t *testing.T) {
	e := newExtractEnv(t)
	ctx := context.Background()

	node, v1 := e.put(t, pgtype.UUID{}, "plan.md", "# Kế hoạch\n\nNgân sách **quý hai** tăng.\n")
	e.drainAll(t)
	ver, content, ok := e.indexed(t, node.ID)
	if !ok || ver != v1.ID {
		t.Fatalf("v1 not indexed: ok=%v", ok)
	}
	// Markdown goes through the block parser: text, not syntax.
	if !strings.Contains(content, "Ngân sách quý hai tăng.") || strings.Contains(content, "**") || strings.Contains(content, "#") {
		t.Errorf("markdown content indexed as %q", content)
	}

	_, v2 := e.put(t, node.ID, "plan.md", "Completely rewritten\n")
	e.drainAll(t)
	if ver, content, _ := e.indexed(t, node.ID); ver != v2.ID || strings.Contains(content, "Ngân") {
		t.Fatalf("v2 did not replace v1: %q", content)
	}

	// A retried job for v1 arriving after v2 is a no-op, not a rollback.
	if err := e.w.Reindex(ctx, v1); err != nil {
		t.Fatal(err)
	}
	e.drainAll(t)
	if ver, _, _ := e.indexed(t, node.ID); ver != v2.ID {
		t.Fatal("a late job for a superseded version overwrote the current text")
	}

	if err := e.fs.RestoreVersion(ctx, node.ID, v1.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	e.drainAll(t)
	if ver, content, _ := e.indexed(t, node.ID); ver != v1.ID || !strings.Contains(content, "Ngân sách") {
		t.Fatalf("restoring v1 did not re-index it: %q", content)
	}

	if err := e.fs.Delete(ctx, node.ID, e.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := e.indexed(t, node.ID); !ok {
		t.Error("trashing dropped the text; restoring from the trash would leave the file unsearchable")
	}
	if _, err := e.fs.Purge(ctx, node.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, _, ok := e.indexed(t, node.ID); ok {
		t.Error("purge left the extracted text behind")
	}
}

func TestPlainTextIsIndexedVerbatimAndOtherTypesAreNot(t *testing.T) {
	e := newExtractEnv(t)
	txt, _ := e.put(t, pgtype.UUID{}, "notes.txt", "line one\nline **two**\n")
	img, _ := e.put(t, pgtype.UUID{}, "photo.bin", "\x00\x01binary")
	e.drainAll(t)
	if _, content, _ := e.indexed(t, txt.ID); content != "line one\nline **two**\n" {
		t.Errorf("plain text indexed as %q", content)
	}
	if _, _, ok := e.indexed(t, img.ID); ok {
		t.Error("a binary file was indexed")
	}
}

// Files stored before content search existed are picked up by the backfill,
// and re-running it does not queue them twice.
func TestBackfillTextIndexesExistingFiles(t *testing.T) {
	e := newExtractEnv(t)
	ctx := context.Background()
	e.fs.SetVersionHook(nil) // as if uploaded before M27
	node, ver := e.put(t, pgtype.UUID{}, "legacy.md", "Old *budget* notes\n")

	jobsFor := func() int {
		t.Helper()
		var n int
		if err := e.pool.QueryRow(ctx,
			`SELECT count(*) FROM jobs WHERE kind = $1 AND payload->>'node_version_id' = $2`,
			KindExtractText, ver.ID.String()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, err := BackfillText(ctx, e.q); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if _, err := BackfillText(ctx, e.q); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if n := jobsFor(); n != 1 {
		t.Fatalf("backfill queued %d jobs for the version, want exactly 1", n)
	}
	e.drainAll(t)
	if _, content, ok := e.indexed(t, node.ID); !ok || !strings.Contains(content, "Old budget notes") {
		t.Fatalf("backfilled file not indexed: %q", content)
	}
	if _, err := BackfillText(ctx, e.q); err != nil {
		t.Fatal(err)
	}
	if n := jobsFor(); n != 1 {
		t.Errorf("an indexed version was queued again (%d jobs)", n)
	}
}
