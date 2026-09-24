package uploads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Expiry lives in SQL (GetUpload filters on it, the sweep deletes by it), so
// this runs against a real Postgres or skips loudly:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/uploads/
func testEnv(t *testing.T) (*Service, *store.Queries, *pgxpool.Pool, store.User, store.Space) {
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
	sub := fmt.Sprintf("test-uploads-%d-%d", os.Getpid(), time.Now().UnixNano())
	u, err := q.CreateUser(ctx, store.CreateUserParams{OidcSub: sub, OidcIss: "test", Email: sub + "@test.local", Name: "uploader"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	sp, err := q.CreateSpace(ctx, store.CreateSpaceParams{Name: sub, OwnerID: u.ID})
	if err != nil {
		t.Fatalf("space: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteSpace(ctx, sp.ID) })
	svc, err := NewService(q, t.TempDir())
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	return svc, q, pool, u, sp
}

func TestSweepRemovesIdleUploadsAndStrayFiles(t *testing.T) {
	svc, _, pool, u, sp := testEnv(t)
	ctx := context.Background()

	idle, err := svc.Create(ctx, CreateParams{SpaceID: sp.ID, Name: "idle.bin", TotalSize: 10, CreatedBy: u.ID})
	if err != nil {
		t.Fatalf("create idle: %v", err)
	}
	live, err := svc.Create(ctx, CreateParams{SpaceID: sp.ID, Name: "live.bin", TotalSize: 10, CreatedBy: u.ID})
	if err != nil {
		t.Fatalf("create live: %v", err)
	}
	if _, err := svc.Append(ctx, live, 0, strings.NewReader("01234")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE uploads SET expires_at = now() - interval '1 minute' WHERE id = $1`, idle.ID); err != nil {
		t.Fatalf("age upload: %v", err)
	}
	// A staging file with no row, older than any upload may idle: what a
	// crash between creating the file and the row leaves behind.
	stray := filepath.Join(svc.dir, "stray")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-TTL - 2*time.Hour)
	if err := os.Chtimes(stray, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, idle.ID, u.ID); err != ErrNotFound {
		t.Fatalf("an expired upload must already be invisible, got %v", err)
	}
	if _, err := svc.Sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM uploads WHERE id = $1`, idle.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("the idle upload's row survived the sweep")
	}
	for _, p := range []string{svc.Path(idle), stray} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived the sweep", filepath.Base(p))
		}
	}
	got, err := svc.Get(ctx, live.ID, u.ID)
	if err != nil {
		t.Fatalf("the live upload was swept: %v", err)
	}
	if got.Uploaded != 5 {
		t.Fatalf("live upload offset %d, want 5", got.Uploaded)
	}
	if _, err := os.Stat(svc.Path(live)); err != nil {
		t.Fatalf("the live upload's bytes were swept: %v", err)
	}
}

// A row that claims more bytes than its staging file holds cannot resume
// without splicing zeros into the file; it must be dropped, not continued.
func TestAppendRefusesARowAheadOfItsFile(t *testing.T) {
	svc, _, _, u, sp := testEnv(t)
	ctx := context.Background()

	up, err := svc.Create(ctx, CreateParams{SpaceID: sp.ID, Name: "a.bin", TotalSize: 10, CreatedBy: u.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Append(ctx, up, 0, strings.NewReader("01234")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.Truncate(svc.Path(up), 2); err != nil {
		t.Fatal(err)
	}
	up, err = svc.Get(ctx, up.ID, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := svc.Append(ctx, up, 5, strings.NewReader("56789")); err != ErrNotFound {
		t.Fatalf("append past the file: got %v, want ErrNotFound", err)
	}
	if _, err := svc.Get(ctx, up.ID, u.ID); err != ErrNotFound {
		t.Fatalf("the inconsistent upload should be gone, got %v", err)
	}
}
