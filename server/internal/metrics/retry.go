package metrics

import "github.com/prometheus/client_golang/prometheus"

type RetryMetrics struct {
	attemptsTotal *prometheus.CounterVec
}

func NewRetryMetrics() *RetryMetrics {
	return &RetryMetrics{
		attemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "retry",
			Name:      "attempts_total",
			Help:      "Total retry attempts by error type.",
		}, []string{"error_type"}),
	}
}

func (m *RetryMetrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{m.attemptsTotal}
}

func (m *RetryMetrics) RecordAttempt(errorType string) {
	if m == nil {
		return
	}
	m.attemptsTotal.WithLabelValues(errorType).Inc()
}
