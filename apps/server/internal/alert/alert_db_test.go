package alert

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nhatbui/pp-hub/apps/server/internal/audit"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
	"github.com/nhatbui/pp-hub/apps/server/internal/sysconfig"
)

// The burst rule is a GROUP BY over the real log, and its once-per-window
// suppression reads back the alert rows it wrote; neither means anything
// against a fake. A real Postgres or a loud skip:
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5433/pphub go test ./internal/alert/
func TestScanRaisesOncePerWindow(t *testing.T) {
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

	// TEXT column, so a per-run suffix keeps this run's addresses its own.
	suffix := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	loud, quiet := "198.51.100.7-"+suffix, "203.0.113.9-"+suffix
	fail := func(ip string, n int) {
		t.Helper()
		for range n {
			if err := q.RecordSystemSecurityEvent(ctx, store.RecordSystemSecurityEventParams{
				Kind: string(audit.KindLoginFailed), Subject: "invalid_state", Ip: ip, Detail: []byte("{}"),
			}); err != nil {
				t.Fatalf("record: %v", err)
			}
		}
	}
	fail(loud, 10) // at the default threshold
	fail(quiet, 9) // one short
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM security_events WHERE ip = ANY($1)`, []string{loud, quiet})
	})

	// Defaults: 10 attempts within 15 minutes. Nil notifier and metrics are
	// both nil-safe.
	w := NewWatcher(q, audit.New(q), sysconfig.NewService(q, nil), nil, nil)

	alertsFor := func(ip string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM security_events WHERE kind = 'security_alert' AND subject = $1 AND ip = $2`,
			RuleFailedLoginBurst, ip).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	if _, err := w.Scan(ctx); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n := alertsFor(loud); n != 1 {
		t.Fatalf("alerts for %s after first scan = %d, want 1", loud, n)
	}
	if n := alertsFor(quiet); n != 0 {
		t.Fatalf("alerts for %s = %d, want 0 below the threshold", quiet, n)
	}

	if _, err := w.Scan(ctx); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if n := alertsFor(loud); n != 1 {
		t.Fatalf("alerts for %s after second scan = %d, want still 1 within the window", loud, n)
	}
}
