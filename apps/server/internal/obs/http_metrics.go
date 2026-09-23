package obs

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics are the request series the API's access-log middleware feeds.
// A nil *HTTPMetrics is valid and records nothing.
//
// The route label is always a chi route pattern or "unmatched", never a raw
// path: node ids and share tokens must not become label values, both for
// cardinality and because a share token is a credential.
type HTTPMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func NewHTTPMetrics(reg prometheus.Registerer) *HTTPMetrics {
	m := &HTTPMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pphub_http_requests_total",
			Help: "HTTP requests served, by method, chi route pattern and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "pphub_http_request_duration_seconds",
			Help: "HTTP request latency, by method and chi route pattern.",
			// Up to five minutes: downloads and uploads stream for that long.
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300},
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pphub_http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.inFlight)
	return m
}

// Start marks a request as in flight.
func (m *HTTPMetrics) Start() {
	if m == nil {
		return
	}
	m.inFlight.Inc()
}

// Done records a finished request that Start was called for.
func (m *HTTPMetrics) Done(method, route string, status int, d time.Duration) {
	if m == nil {
		return
	}
	m.inFlight.Dec()
	method = normalizeMethod(method)
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(d.Seconds())
}

// normalizeMethod bounds the method label: the verb is caller-chosen, and an
// arbitrary one must not mint a new series.
func normalizeMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return m
	}
	return "OTHER"
}
