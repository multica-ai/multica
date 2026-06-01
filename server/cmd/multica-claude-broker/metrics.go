package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Outcomes for the refresh and access-token metrics. Kept as constants so
// label cardinality stays bounded and typos don't accidentally create new
// label values.
const (
	outcomeOk      = "ok"
	outcomeError   = "error"
	outcomeSkipped = "skipped"
	outcomeStale   = "stale"
)

var (
	refreshTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "multica_claude_broker_refresh_total",
			Help: "Total OAuth refresh attempts by outcome.",
		},
		[]string{"outcome"},
	)
	refreshFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "multica_claude_broker_refresh_failures_total",
			Help: "Refresh failures by classification (transient|permanent|not_leader).",
		},
		[]string{"reason"},
	)
	refreshDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "multica_claude_broker_refresh_duration_seconds",
			Help:    "Wall-clock duration of refresh attempts (including retries).",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
	)
	accessTokenExpiresAt = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "multica_claude_broker_access_token_expires_at_seconds",
			Help: "Unix timestamp at which the current access_token expires.",
		},
	)
	accessTokenRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "multica_claude_broker_access_token_requests_total",
			Help: "GET /access_token requests by outcome (ok|error|stale).",
		},
		[]string{"outcome"},
	)
	leaderStateGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "multica_claude_broker_leader",
			Help: "1 if this pod currently holds the refresh lease, else 0.",
		},
	)
	constantsInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "multica_claude_broker_constants_info",
			Help: "Build-time embedded OAuth constants metadata. Always 1; labels carry version + extracted_at.",
		},
		[]string{"claude_version", "extracted_at", "version_header"},
	)
)

func init() {
	// One-shot info metric — populated at startup, never updated.
	constantsInfo.WithLabelValues(
		Constants.ClaudeVersion,
		Constants.ExtractedAt,
		Constants.VersionHeader,
	).Set(1)
}

// NewMetricsMux returns the /metrics handler on its own mux so it can bind
// to a dedicated port.
func NewMetricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
