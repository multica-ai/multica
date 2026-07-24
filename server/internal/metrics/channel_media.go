package metrics

import "github.com/prometheus/client_golang/prometheus"

// ChannelMediaReconcilerMetrics observes the media intent-ledger reconciler:
// how many unreferenced objects it deletes, how many rows it clears because a
// durable attachment reference exists, how many storage deletes fail (and go
// to backoff), and the current ledger backlog. Whether the fixed settle/sweep
// constants ever need a config surface is decided from these numbers.
type ChannelMediaReconcilerMetrics struct {
	ObjectsDeleted prometheus.Counter
	RowsReferenced prometheus.Counter
	DeleteFailures prometheus.Counter
	Backlog        prometheus.Gauge
}

func NewChannelMediaReconcilerMetrics() *ChannelMediaReconcilerMetrics {
	return &ChannelMediaReconcilerMetrics{
		ObjectsDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_media",
			Name:      "reconciler_objects_deleted_total",
			Help:      "Unreferenced media objects deleted by the reconciler.",
		}),
		RowsReferenced: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_media",
			Name:      "reconciler_rows_referenced_total",
			Help:      "Ledger rows cleared because a durable attachment references the object.",
		}),
		DeleteFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_media",
			Name:      "reconciler_delete_failures_total",
			Help:      "Object-storage deletes that failed and were scheduled for retry.",
		}),
		Backlog: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel_media",
			Name:      "pending_objects",
			Help:      "Rows currently in the media intent ledger.",
		}),
	}
}

func (m *ChannelMediaReconcilerMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.ObjectsDeleted, m.RowsReferenced, m.DeleteFailures, m.Backlog}
}
