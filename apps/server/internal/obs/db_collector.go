package obs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// dbStatsTTL bounds how often a scrape may query Postgres. The pool figures
// are read fresh every scrape; they cost no I/O.
const dbStatsTTL = 30 * time.Second

var (
	descPoolTotal    = prometheus.NewDesc("pphub_db_pool_total_conns", "Connections in the Postgres pool.", nil, nil)
	descPoolAcquired = prometheus.NewDesc("pphub_db_pool_acquired_conns", "Pool connections currently checked out.", nil, nil)
	descPoolIdle     = prometheus.NewDesc("pphub_db_pool_idle_conns", "Idle pool connections.", nil, nil)
	descPoolMax      = prometheus.NewDesc("pphub_db_pool_max_conns", "Maximum size of the pool.", nil, nil)
	descAcquires     = prometheus.NewDesc("pphub_db_pool_acquires_total", "Successful pool acquires.", nil, nil)
	descEmpty        = prometheus.NewDesc("pphub_db_pool_empty_acquires_total", "Acquires that had to wait because the pool was empty.", nil, nil)
	descAcquireWait  = prometheus.NewDesc("pphub_db_pool_acquire_wait_seconds_total", "Total time spent acquiring pool connections.", nil, nil)

	descJobs       = prometheus.NewDesc("pphub_jobs", "Jobs not yet done, by kind and status.", []string{"kind", "status"}, nil)
	descJobsOldest = prometheus.NewDesc("pphub_jobs_oldest_age_seconds", "Age of the oldest job, by kind and status.", []string{"kind", "status"}, nil)
	descBlobs      = prometheus.NewDesc("pphub_blobs", "Stored blobs, by state (referenced, unreferenced = awaiting GC).", []string{"state"}, nil)
	descBlobBytes  = prometheus.NewDesc("pphub_blob_bytes", "Stored blob bytes, by state (referenced, unreferenced = awaiting GC).", []string{"state"}, nil)
	descBlobDamage = prometheus.NewDesc("pphub_blobs_damaged", "Blobs the scrub found missing or not matching their hash.", nil, nil)
	descDBStatsUp  = prometheus.NewDesc("pphub_db_stats_up", "1 if the last job/blob stats query succeeded, else 0.", nil, nil)
)

// dbCollector reports the Postgres pool and the DB-derived gauges. A nil pool
// or queries omits that half.
type dbCollector struct {
	pool *pgxpool.Pool
	q    *store.Queries

	mu        sync.Mutex
	refreshed time.Time
	up        bool
	jobs      []store.JobQueueStatsRow
	blobs     *store.BlobTotalsRow
}

func newDBCollector(pool *pgxpool.Pool, q *store.Queries) *dbCollector {
	return &dbCollector{pool: pool, q: q}
}

func (c *dbCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		descPoolTotal, descPoolAcquired, descPoolIdle, descPoolMax, descAcquires, descEmpty, descAcquireWait,
		descJobs, descJobsOldest, descBlobs, descBlobBytes, descBlobDamage, descDBStatsUp,
	} {
		ch <- d
	}
}

func (c *dbCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool != nil {
		st := c.pool.Stat()
		gauge := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
		}
		counter := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
		}
		gauge(descPoolTotal, float64(st.TotalConns()))
		gauge(descPoolAcquired, float64(st.AcquiredConns()))
		gauge(descPoolIdle, float64(st.IdleConns()))
		gauge(descPoolMax, float64(st.MaxConns()))
		counter(descAcquires, float64(st.AcquireCount()))
		counter(descEmpty, float64(st.EmptyAcquireCount()))
		counter(descAcquireWait, st.AcquireDuration().Seconds())
	}
	if c.q == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh()
	for _, j := range c.jobs {
		ch <- prometheus.MustNewConstMetric(descJobs, prometheus.GaugeValue, float64(j.Jobs), j.Kind, j.Status)
		ch <- prometheus.MustNewConstMetric(descJobsOldest, prometheus.GaugeValue, j.OldestAgeSeconds, j.Kind, j.Status)
	}
	if b := c.blobs; b != nil {
		ch <- prometheus.MustNewConstMetric(descBlobs, prometheus.GaugeValue, float64(b.ReferencedBlobs), "referenced")
		ch <- prometheus.MustNewConstMetric(descBlobs, prometheus.GaugeValue, float64(b.UnreferencedBlobs), "unreferenced")
		ch <- prometheus.MustNewConstMetric(descBlobBytes, prometheus.GaugeValue, float64(b.ReferencedBytes), "referenced")
		ch <- prometheus.MustNewConstMetric(descBlobBytes, prometheus.GaugeValue, float64(b.UnreferencedBytes), "unreferenced")
		ch <- prometheus.MustNewConstMetric(descBlobDamage, prometheus.GaugeValue, float64(b.DamagedBlobs))
	}
	up := 0.0
	if c.up {
		up = 1
	}
	ch <- prometheus.MustNewConstMetric(descDBStatsUp, prometheus.GaugeValue, up)
}

// refresh re-runs the stats queries when the cache is older than dbStatsTTL.
// The timestamp moves on failure too, so a down database is not queried on
// every scrape; the cached rows are dropped rather than served stale. Callers
// hold c.mu.
func (c *dbCollector) refresh() {
	if !c.refreshed.IsZero() && time.Since(c.refreshed) < dbStatsTTL {
		return
	}
	c.refreshed = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jobs, err := c.q.JobQueueStats(ctx)
	var blobs store.BlobTotalsRow
	if err == nil {
		blobs, err = c.q.BlobTotals(ctx)
	}
	if err != nil {
		slog.WarnContext(ctx, "metrics db refresh failed", "err", err)
		c.up, c.jobs, c.blobs = false, nil, nil
		return
	}
	c.up, c.jobs, c.blobs = true, jobs, &blobs
}
