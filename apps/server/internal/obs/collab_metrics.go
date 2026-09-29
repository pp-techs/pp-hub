package obs

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CollabMetrics are the block-document collaboration series (M39): how many
// rooms are resident and how many sockets they hold, and how the write-back
// of a room's document to a version fares. A nil *CollabMetrics is valid and
// records nothing.
type CollabMetrics struct {
	rooms         prometheus.Gauge
	peers         prometheus.Gauge
	flushDuration prometheus.Histogram
	flushFailures prometheus.Counter
}

func NewCollabMetrics(reg prometheus.Registerer) *CollabMetrics {
	m := &CollabMetrics{
		rooms: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pphub_collab_rooms",
			Help: "Block-document rooms resident in memory, including idle ones kept warm.",
		}),
		peers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pphub_collab_peers",
			Help: "Collaboration sockets connected to a room.",
		}),
		flushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "pphub_collab_flush_duration_seconds",
			Help:    "Time to serialize a room and write it back, whether or not a version resulted.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}),
		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "pphub_collab_flush_failures_total",
			Help: "Room write-backs that failed and are retained in memory for a retry.",
		}),
	}
	reg.MustRegister(m.rooms, m.peers, m.flushDuration, m.flushFailures)
	return m
}

// RoomLoaded counts a room becoming resident. Nil-safe.
func (m *CollabMetrics) RoomLoaded() {
	if m != nil {
		m.rooms.Inc()
	}
}

// RoomUnloaded counts a resident room going away. Nil-safe.
func (m *CollabMetrics) RoomUnloaded() {
	if m != nil {
		m.rooms.Dec()
	}
}

// PeerJoined counts a socket admitted to a room. Nil-safe.
func (m *CollabMetrics) PeerJoined() {
	if m != nil {
		m.peers.Inc()
	}
}

// PeerLeft counts a socket that PeerJoined counted going away. Nil-safe.
func (m *CollabMetrics) PeerLeft() {
	if m != nil {
		m.peers.Dec()
	}
}

// Flushed records one write-back attempt and whether it failed. Nil-safe.
func (m *CollabMetrics) Flushed(d time.Duration, failed bool) {
	if m == nil {
		return
	}
	m.flushDuration.Observe(d.Seconds())
	if failed {
		m.flushFailures.Inc()
	}
}
