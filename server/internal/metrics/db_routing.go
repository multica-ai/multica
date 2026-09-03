package metrics

import "github.com/prometheus/client_golang/prometheus"

type DBRoutingMetrics struct {
	readRoutes *prometheus.CounterVec
}

func NewDBRoutingMetrics() *DBRoutingMetrics {
	return &DBRoutingMetrics{
		readRoutes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "multica_db_read_routes_total",
			Help: "Read selections by business, database role, and bounded reason.",
		}, []string{"business", "role", "reason"}),
	}
}

func (m *DBRoutingMetrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{m.readRoutes}
}

func (m *DBRoutingMetrics) RecordReadRoute(business, role, reason string) {
	if m == nil {
		return
	}
	m.readRoutes.WithLabelValues(business, role, reason).Inc()
}
