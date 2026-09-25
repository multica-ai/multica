package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ChannelLeaseMetrics exposes the ownership signals needed to verify a Redis
// lease cutover without putting installation IDs into metric labels.
type ChannelLeaseMetrics struct {
	Operations              *prometheus.CounterVec
	ActiveOwners            prometheus.Gauge
	OwnersWithRenewalErrors prometheus.Gauge
	LastSuccessfulRenew     prometheus.Gauge
	TakeoverLatency         prometheus.Histogram
}

func NewChannelLeaseMetrics() *ChannelLeaseMetrics {
	return &ChannelLeaseMetrics{
		Operations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "channel_lease",
			Name:      "operations_total",
			Help:      "Channel lease operations by operation and outcome.",
		}, []string{"operation", "outcome"}),
		ActiveOwners: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel_lease",
			Name:      "active_owners",
			Help:      "Channel installations currently owned by this process.",
		}),
		OwnersWithRenewalErrors: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel_lease",
			Name:      "owners_with_renewal_errors",
			Help:      "Channel lease owners whose latest renewal attempt failed.",
		}),
		LastSuccessfulRenew: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "channel_lease",
			Name:      "last_successful_renewal_timestamp_seconds",
			Help:      "Unix timestamp of the most recent successful channel lease renewal.",
		}),
		TakeoverLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "channel_lease",
			Name:      "takeover_latency_seconds",
			Help:      "Time from first observed contention until this process acquires the lease.",
			Buckets:   []float64{1, 5, 15, 30, 60, 120, 180, 300},
		}),
	}
}

func (m *ChannelLeaseMetrics) RecordLeaseOperation(operation, outcome string) {
	if m != nil {
		m.Operations.WithLabelValues(operation, outcome).Inc()
	}
}

func (m *ChannelLeaseMetrics) SetActiveLeaseOwners(count float64) {
	if m != nil {
		m.ActiveOwners.Set(count)
	}
}

func (m *ChannelLeaseMetrics) SetOwnersWithRenewalErrors(count float64) {
	if m != nil {
		m.OwnersWithRenewalErrors.Set(count)
	}
}

func (m *ChannelLeaseMetrics) SetLastSuccessfulRenewal(at time.Time) {
	if m != nil {
		m.LastSuccessfulRenew.Set(float64(at.Unix()))
	}
}

func (m *ChannelLeaseMetrics) ObserveTakeoverLatency(delay time.Duration) {
	if m != nil {
		m.TakeoverLatency.Observe(delay.Seconds())
	}
}

func (m *ChannelLeaseMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.Operations, m.ActiveOwners, m.OwnersWithRenewalErrors, m.LastSuccessfulRenew, m.TakeoverLatency}
}
