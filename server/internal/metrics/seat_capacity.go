package metrics

import "github.com/prometheus/client_golang/prometheus"

var seatCapacityActions = []string{
	"reserve_invitation",
	"consume_invitation",
	"claim_share_join",
	"confirm",
	"release",
	"release_member",
}

// SeatCapacityMetrics exposes product-side billing intents without using
// workspace IDs as labels. Operators can alert on an action becoming old or
// entering the terminal dead-letter state while preserving tenant privacy.
type SeatCapacityMetrics struct {
	Pending          *prometheus.GaugeVec
	DeadLettered     *prometheus.GaugeVec
	OldestPendingAge *prometheus.GaugeVec
}

func NewSeatCapacityMetrics() *SeatCapacityMetrics {
	m := &SeatCapacityMetrics{
		Pending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "pending",
			Help:      "Product-side seat capacity intents waiting to settle by action.",
		}, []string{"action"}),
		DeadLettered: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "dead_lettered",
			Help:      "Terminal seat capacity intents requiring operator repair by action.",
		}, []string{"action"}),
		OldestPendingAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "oldest_pending_age_seconds",
			Help:      "Age in seconds of the oldest unsettled seat capacity intent by action.",
		}, []string{"action"}),
	}
	m.ResetOutbox()
	return m
}

func (m *SeatCapacityMetrics) ResetOutbox() {
	if m == nil {
		return
	}
	m.Pending.Reset()
	m.DeadLettered.Reset()
	m.OldestPendingAge.Reset()
	for _, action := range seatCapacityActions {
		m.SetOutbox(action, 0, 0, 0)
	}
}

func (m *SeatCapacityMetrics) SetOutbox(action string, pending, deadLettered int64, oldestPendingAgeSeconds float64) {
	if m == nil {
		return
	}
	m.Pending.WithLabelValues(action).Set(float64(pending))
	m.DeadLettered.WithLabelValues(action).Set(float64(deadLettered))
	m.OldestPendingAge.WithLabelValues(action).Set(oldestPendingAgeSeconds)
}

func (m *SeatCapacityMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.Pending, m.DeadLettered, m.OldestPendingAge}
}
