package preview

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// testQueries connects to PP_TEST_DATABASE_URL, or skips.
//
// The rest of this repo's tests are pure units and touch no database. The
// kinds filter cannot be tested that way — it lives in SQL — and it is the
// single assertion that makes "the worker is optional" true rather than
// merely intended, so it gets a real Postgres or it gets skipped loudly.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5432/pphub_test go test ./internal/preview/
func testQueries(t *testing.T) (*store.Queries, *pgxpool.Pool) {
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
	if _, err := pool.Exec(ctx, "DELETE FROM jobs"); err != nil {
		t.Fatalf("clear jobs: %v", err)
	}
	return store.New(pool), pool
}

// The core contract: a process must not claim a job kind it has no renderer
// for. Without this, a deployment with no cmd/worker claims every render:pdf,
// fails it maxAttempts times and parks it in 'failed' — permanently, since
// Enqueue only ever fires on upload.
func TestClaimJobsFiltersByKind(t *testing.T) {
	q, pool := testQueries(t)
	ctx := context.Background()

	for _, kind := range []string{KindRenderPDF, KindRenderVideo, KindRenderImage} {
		if _, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: kind, Payload: []byte(`{}`)}); err != nil {
			t.Fatalf("enqueue %s: %v", kind, err)
		}
	}

	// A server-shaped process: pure-Go renderers only.
	claimed, err := q.ClaimJobs(ctx, store.ClaimJobsParams{
		Kinds: []string{KindRenderImage, KindSnippetText},
		Lim:   10,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs, want 1", len(claimed))
	}
	if claimed[0].Kind != KindRenderImage {
		t.Errorf("claimed %q, want %q", claimed[0].Kind, KindRenderImage)
	}

	// The two it could not handle must still be pending — not failed, not
	// attempted. A worker deployed later has to find them intact.
	var pending int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE status = 'pending' AND attempts = 0 AND kind = ANY($1::text[])`,
		[]string{KindRenderPDF, KindRenderVideo},
	).Scan(&pending)
	if err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 2 {
		t.Errorf("%d unclaimable jobs left pending and untouched, want 2", pending)
	}

	// And a worker-shaped process picks up exactly those.
	claimed, err = q.ClaimJobs(ctx, store.ClaimJobsParams{
		Kinds: []string{KindRenderPDF, KindRenderVideo},
		Lim:   10,
	})
	if err != nil {
		t.Fatalf("claim as worker: %v", err)
	}
	if len(claimed) != 2 {
		t.Errorf("worker claimed %d jobs, want 2", len(claimed))
	}
}

// A worker that dies mid-render strands its job in 'running'. Nothing but the
// reaper moves it back, so without this the job is lost silently — the file
// simply never gets a thumbnail and no error is recorded anywhere.
func TestReapStuckJobs(t *testing.T) {
	q, pool := testQueries(t)
	ctx := context.Background()

	// Three jobs, all claimed (so all 'running'), then aged differently.
	var ids []pgtype.UUID
	for range 3 {
		j, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindRenderPDF, Payload: []byte(`{}`)})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, j.ID)
	}
	if _, err := q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: []string{KindRenderPDF}, Lim: 10}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// ids[0] stays fresh — a worker is plausibly still on it.
	// ids[1] is stale with attempts left  → back to pending.
	// ids[2] is stale with attempts spent → failed, not looped forever.
	age := func(id pgtype.UUID, attempts int32) {
		if _, err := pool.Exec(ctx,
			`UPDATE jobs SET run_at = now() - interval '1 hour', attempts = $2 WHERE id = $1`,
			id, attempts); err != nil {
			t.Fatalf("age job: %v", err)
		}
	}
	age(ids[1], 1)
	age(ids[2], maxAttempts)

	n, err := q.ReapStuckJobs(ctx, store.ReapStuckJobsParams{
		MaxAttempts:  maxAttempts,
		LeaseSeconds: int32(jobLease.Seconds()),
	})
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 2 {
		t.Errorf("reaped %d jobs, want 2 (the fresh one must be left alone)", n)
	}

	status := func(id pgtype.UUID) string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT status::text FROM jobs WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read status: %v", err)
		}
		return s
	}
	if got := status(ids[0]); got != "running" {
		t.Errorf("fresh job = %q, want running — reaping it would let a second worker duplicate the render", got)
	}
	if got := status(ids[1]); got != "pending" {
		t.Errorf("stale job with attempts left = %q, want pending", got)
	}
	if got := status(ids[2]); got != "failed" {
		t.Errorf("stale job with attempts spent = %q, want failed — otherwise it is reclaimed forever", got)
	}
}

// ClaimJobs must stamp run_at, or a job enqueued long ago (a backfill) looks
// stale the moment it is claimed and gets reaped out from under its worker.
func TestClaimStampsRunAtSoBacklogIsNotReapedInstantly(t *testing.T) {
	q, pool := testQueries(t)
	ctx := context.Background()

	j, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindRenderPDF, Payload: []byte(`{}`)})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Backdate it, as a backfilled job sitting in the queue would be.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET run_at = now() - interval '3 hours' WHERE id = $1`, j.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := q.ClaimJobs(ctx, store.ClaimJobsParams{Kinds: []string{KindRenderPDF}, Lim: 10}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	n, err := q.ReapStuckJobs(ctx, store.ReapStuckJobsParams{
		MaxAttempts:  maxAttempts,
		LeaseSeconds: int32(jobLease.Seconds()),
	})
	if err != nil {
		t.Fatalf("reap: %v", err)
	}
	if n != 0 {
		t.Errorf("reaped %d just-claimed jobs, want 0", n)
	}
}

// Only finished rows past retention go: failed rows feed admin stats and
// metrics, and pending or recent rows are still the queue.
func TestPurgeDoneJobsKeepsUnfinishedAndRecentRows(t *testing.T) {
	q, pool := testQueries(t)
	ctx := context.Background()

	set := func(status, age string) pgtype.UUID {
		t.Helper()
		j, err := q.EnqueueJob(ctx, store.EnqueueJobParams{Kind: KindRenderPDF, Payload: []byte(`{}`)})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE jobs SET status = $2::job_status, created_at = now() - $3::interval WHERE id = $1`,
			j.ID, status, age); err != nil {
			t.Fatalf("set job: %v", err)
		}
		return j.ID
	}
	doneOld := set("done", "8 days")
	keep := []pgtype.UUID{
		set("done", "0 seconds"),
		set("failed", "8 days"),
		set("pending", "8 days"),
	}

	n, err := PurgeDoneJobs(ctx, q, time.Now().Add(-DoneJobRetention))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d jobs, want 1", n)
	}
	exists := func(id pgtype.UUID) bool {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE id = $1)`, id).Scan(&ok); err != nil {
			t.Fatalf("exists: %v", err)
		}
		return ok
	}
	if exists(doneOld) {
		t.Error("the old finished job survived the purge")
	}
	for i, id := range keep {
		if !exists(id) {
			t.Errorf("job %d was purged; only old done rows may go", i)
		}
	}
}
