package obs

import (
	"log/slog"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	descDiskFree = prometheus.NewDesc("pphub_blob_disk_free_bytes",
		"Bytes available to the server on the filesystem holding PP_BLOB_ROOT.", nil, nil)
	descDiskSize = prometheus.NewDesc("pphub_blob_disk_size_bytes",
		"Size of the filesystem holding PP_BLOB_ROOT.", nil, nil)
)

// diskCollector reports the filesystem under the blob root. With the local
// backend that is where every blob lives; with S3 it still holds resumable
// uploads in flight. Either way a full disk otherwise shows up first as
// failed uploads, and this is what an alert can watch instead.
type diskCollector struct {
	path string
}

func (c diskCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descDiskFree
	ch <- descDiskSize
}

func (c diskCollector) Collect(ch chan<- prometheus.Metric) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(c.path, &st); err != nil {
		slog.Warn("metrics: statfs blob root failed", "path", c.path, "err", err)
		return
	}
	bsize := uint64(st.Bsize)
	ch <- prometheus.MustNewConstMetric(descDiskFree, prometheus.GaugeValue, float64(uint64(st.Bavail)*bsize))
	ch <- prometheus.MustNewConstMetric(descDiskSize, prometheus.GaugeValue, float64(uint64(st.Blocks)*bsize))
}
