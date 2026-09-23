package obs

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// The job and blob gauges are SQL; they get a real Postgres or are skipped.
//
//	PP_TEST_DATABASE_URL=postgres://pphub:pphub@localhost:5432/pphub?sslmode=disable go test ./internal/obs/
func TestDBCollectorReportsQueueAndBlobs(t *testing.T) {
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

	const hash = "test-metrics-unreferenced-blob"
	if _, err := pool.Exec(ctx, `INSERT INTO jobs (kind) VALUES ('test:metrics')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO blobs (hash, size, ref_count) VALUES ($1, 42, 0)`, hash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'test:metrics'`)
		_, _ = pool.Exec(ctx, `DELETE FROM blobs WHERE hash = $1`, hash)
	})

	reg := prometheus.NewRegistry()
	reg.MustRegister(newDBCollector(pool, store.New(pool)))
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}

	value := func(name string, labels map[string]string) (float64, bool) {
		for _, f := range families {
			if f.GetName() != name {
				continue
			}
			for _, m := range f.GetMetric() {
				if matches(m, labels) {
					return m.GetGauge().GetValue(), true
				}
			}
		}
		return 0, false
	}
	if v, ok := value("pphub_jobs", map[string]string{"kind": "test:metrics", "status": "pending"}); !ok || v != 1 {
		t.Errorf("pphub_jobs{kind=test:metrics,status=pending} = %v (present %v), want 1", v, ok)
	}
	if v, ok := value("pphub_blobs", map[string]string{"state": "unreferenced"}); !ok || v < 1 {
		t.Errorf("pphub_blobs{state=unreferenced} = %v (present %v), want >= 1", v, ok)
	}
	if v, ok := value("pphub_blob_bytes", map[string]string{"state": "unreferenced"}); !ok || v < 42 {
		t.Errorf("pphub_blob_bytes{state=unreferenced} = %v (present %v), want >= 42", v, ok)
	}
	if v, _ := value("pphub_db_stats_up", nil); v != 1 {
		t.Errorf("pphub_db_stats_up = %v, want 1", v)
	}
	var hasPool bool
	for _, f := range families {
		hasPool = hasPool || strings.HasPrefix(f.GetName(), "pphub_db_pool_")
	}
	if !hasPool {
		t.Error("no pool series")
	}
}

func matches(m *dto.Metric, labels map[string]string) bool {
	n := 0
	for _, lp := range m.GetLabel() {
		if want, ok := labels[lp.GetName()]; ok {
			if lp.GetValue() != want {
				return false
			}
			n++
		}
	}
	return n == len(labels)
}
