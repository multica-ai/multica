package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewHandler(gatherer prometheus.Gatherer) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		ErrorHandling:     promhttp.HTTPErrorOnError,
	}))
	return mux
}

func NewServer(addr string, gatherer prometheus.Gatherer) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewHandler(gatherer),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
