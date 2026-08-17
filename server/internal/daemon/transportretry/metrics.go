package transportretry

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	transportRetryMetrics     *Metrics
	transportRetryMetricsOnce sync.Once
)

// Metrics exposes Prometheus counters/histograms for in-turn transport retries.
type Metrics struct {
	attemptTotal      *prometheus.CounterVec
	recoveredTotal    *prometheus.CounterVec
	cacheReadTokens   *prometheus.CounterVec
	wallSeconds       *prometheus.HistogramVec
}

// GlobalMetrics returns the process-wide transport-retry metrics registrar.
func GlobalMetrics() *Metrics {
	transportRetryMetricsOnce.Do(func() {
		transportRetryMetrics = newMetrics()
	})
	return transportRetryMetrics
}

func newMetrics() *Metrics {
	m := &Metrics{
		attemptTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_transport_retry",
			Name:      "attempt_total",
			Help:      "In-turn transport retry attempts by provider, policy, session mode, and outcome.",
		}, []string{"provider", "policy_id", "session_mode", "outcome"}),
		recoveredTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_transport_retry",
			Name:      "recovered_total",
			Help:      "In-turn transport retries that recovered without surfacing a failed task.",
		}, []string{"provider", "policy_id", "session_mode"}),
		cacheReadTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "agent_transport_retry",
			Name:      "cache_read_tokens",
			Help:      "Cache-read tokens observed on recovered transport-retry attempts.",
		}, []string{"provider", "policy_id"}),
		wallSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "agent_transport_retry",
			Name:      "wall_seconds",
			Help:      "Extra wall-clock seconds spent in in-turn transport retries.",
			Buckets:   []float64{1, 2, 5, 10, 30, 60, 120, 300, 600},
		}, []string{"provider", "policy_id"}),
	}
	prometheus.MustRegister(
		m.attemptTotal,
		m.recoveredTotal,
		m.cacheReadTokens,
		m.wallSeconds,
	)
	return m
}

// Collectors returns registered collectors for tests.
func (m *Metrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{
		m.attemptTotal,
		m.recoveredTotal,
		m.cacheReadTokens,
		m.wallSeconds,
	}
}

// RecordAttempt implements Observer.
func (m *Metrics) RecordAttempt(provider, policyID, sessionMode, outcome string) {
	if m == nil {
		return
	}
	m.attemptTotal.WithLabelValues(normalizeProvider(provider), normalizePolicy(policyID), normalizeSessionMode(sessionMode), normalizeOutcome(outcome)).Inc()
}

// RecordRecovered implements Observer.
func (m *Metrics) RecordRecovered(provider, policyID, sessionMode string) {
	if m == nil {
		return
	}
	m.recoveredTotal.WithLabelValues(normalizeProvider(provider), normalizePolicy(policyID), normalizeSessionMode(sessionMode)).Inc()
}

// RecordCacheReadTokens implements Observer.
func (m *Metrics) RecordCacheReadTokens(provider, policyID string, tokens int64) {
	if m == nil || tokens <= 0 {
		return
	}
	m.cacheReadTokens.WithLabelValues(normalizeProvider(provider), normalizePolicy(policyID)).Add(float64(tokens))
}

// RecordWallSeconds implements Observer.
func (m *Metrics) RecordWallSeconds(provider, policyID string, seconds float64) {
	if m == nil || seconds < 0 {
		return
	}
	m.wallSeconds.WithLabelValues(normalizeProvider(provider), normalizePolicy(policyID)).Observe(seconds)
}

func normalizeProvider(provider string) string {
	if provider == "" {
		return "unknown"
	}
	return provider
}

func normalizePolicy(id string) string {
	if id == "" {
		return "none"
	}
	return id
}

func normalizeSessionMode(mode string) string {
	if mode == "" {
		return "none"
	}
	return mode
}

func normalizeOutcome(outcome string) string {
	if outcome == "" {
		return "unknown"
	}
	return outcome
}

// MetricsObserver wraps Metrics to finish wall-clock recording at end of retry loop.
type MetricsObserver struct {
	Metrics *Metrics
}

func (o MetricsObserver) RecordAttempt(provider, policyID, sessionMode, outcome string) {
	o.Metrics.RecordAttempt(provider, policyID, sessionMode, outcome)
}

func (o MetricsObserver) RecordRecovered(provider, policyID, sessionMode string) {
	o.Metrics.RecordRecovered(provider, policyID, sessionMode)
}

func (o MetricsObserver) RecordCacheReadTokens(provider, policyID string, tokens int64) {
	o.Metrics.RecordCacheReadTokens(provider, policyID, tokens)
}

func (o MetricsObserver) RecordWallSeconds(provider, policyID string, seconds float64) {
	o.Metrics.RecordWallSeconds(provider, policyID, seconds)
}
