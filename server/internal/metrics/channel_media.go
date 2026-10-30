package metrics

import "github.com/prometheus/client_golang/prometheus"

// ChannelMediaReconcilerMetrics observes the media intent-ledger reconciler:
// how many unreferenced objects it deletes, how many rows it clears because a
// durable attachment reference exists, and how many storage deletes fail (and
// go to backoff).
type ChannelMediaReconcilerMetrics struct {
	ObjectsDeleted prometheus.Counter
	RowsReferenced prometheus.Counter
	DeleteFailures prometheus.Counter
	// TombstoneReferenced counts an invariant violation, not routine work:
	// a tombstone pass found an attachment reading the object it was about to
	// re-delete. The object is kept; a non-zero value means the per-message
	// key or the bind's state guard stopped holding and needs investigation.
	TombstoneReferenced prometheus.Counter
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
		TombstoneReferenced: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_media",
			Name:      "reconciler_tombstone_referenced_total",
			Help:      "Tombstone passes that found an attachment referencing the object (invariant violation; the object is kept).",
		}),
	}
}

func (m *ChannelMediaReconcilerMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.ObjectsDeleted, m.RowsReferenced, m.DeleteFailures, m.TombstoneReferenced}
}
