package obs

import "github.com/prometheus/client_golang/prometheus"

// AlertMetrics counts security alerts, so an alerting stack that already
// scrapes /metrics can page on them without reading the security log.
type AlertMetrics struct {
	raised *prometheus.CounterVec
}

func NewAlertMetrics(reg prometheus.Registerer) *AlertMetrics {
	m := &AlertMetrics{
		raised: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pphub_security_alerts_total",
			Help: "Security alerts raised, by rule.",
		}, []string{"rule"}),
	}
	reg.MustRegister(m.raised)
	// Pre-created so the series exists at 0: a rate() over a series that
	// appears only on the first alert misses that first alert.
	m.raised.WithLabelValues("login_failed_burst")
	return m
}

// Raised counts one alert for rule. Nil-safe.
func (m *AlertMetrics) Raised(rule string) {
	if m == nil {
		return
	}
	m.raised.WithLabelValues(rule).Inc()
}
