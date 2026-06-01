package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	syncTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "multica_repocache_sync_total",
		Help: "Number of sync attempts grouped by workspace and outcome.",
	}, []string{"workspace_id", "outcome"})

	fetchDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "multica_repocache_fetch_seconds",
		Help:    "Per-workspace Cache.Sync duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"workspace_id"})
)

func init() {
	prometheus.MustRegister(syncTotal, fetchDuration)
}

// NewMetricsMux returns the stdlib mux serving Prometheus on /metrics.
func NewMetricsMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}
