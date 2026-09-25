package obs

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Metrics is the API server's Prometheus registry: Go runtime, process,
// Postgres pool, job queue, blob totals, the blob disk, the HTTP series and
// security alerts.
type Metrics struct {
	Registry *prometheus.Registry
	HTTP     *HTTPMetrics
	Alerts   *AlertMetrics
}

// NewMetrics builds the registry; blobRoot is the directory whose filesystem
// the disk gauges report, "" to omit them.
func NewMetrics(pool *pgxpool.Pool, q *store.Queries, blobRoot string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		newDBCollector(pool, q),
	)
	if blobRoot != "" {
		reg.MustRegister(diskCollector{path: blobRoot})
	}
	return &Metrics{Registry: reg, HTTP: NewHTTPMetrics(reg), Alerts: NewAlertMetrics(reg)}
}

// Serve runs the internal metrics listener until ctx is done. It serves only
// GET /metrics, on its own port so it cannot become public through a routing
// mistake in the edge proxy.
func (m *Metrics) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
