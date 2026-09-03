package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type DBRoutingMetrics struct {
	replicaConfigured prometheus.Gauge
	replicaHealthy    prometheus.Gauge
	replicaLagBytes   prometheus.Gauge
	replicaReplayLag  prometheus.Gauge
	replicaProbes     *prometheus.CounterVec
	readRoutes        *prometheus.CounterVec
	readFallbacks     *prometheus.CounterVec
}

func NewDBRoutingMetrics() *DBRoutingMetrics {
	return &DBRoutingMetrics{
		replicaConfigured: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "multica_db_replica_configured",
			Help: "Whether a PostgreSQL read replica is configured for this API process.",
		}),
		replicaHealthy: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "multica_db_replica_healthy",
			Help: "Whether the configured PostgreSQL replica is currently eligible for reads.",
		}),
		replicaLagBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "multica_db_replica_lag_bytes",
			Help: "WAL byte distance between the primary probe position and the replica replay position.",
		}),
		replicaReplayLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "multica_db_replica_replay_lag_seconds",
			Help: "Age of the last replayed transaction while the replica is behind the primary probe position.",
		}),
		replicaProbes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "multica_db_replica_probes_total",
			Help: "Replica eligibility probes by result and bounded reason.",
		}, []string{"result", "reason"}),
		readRoutes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "multica_db_read_routes_total",
			Help: "Read selections by business, database role, and bounded reason.",
		}, []string{"business", "role", "reason"}),
		readFallbacks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "multica_db_replica_fallbacks_total",
			Help: "Replica reads retried on primary by business and bounded reason.",
		}, []string{"business", "reason"}),
	}
}

func (m *DBRoutingMetrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}
	return []prometheus.Collector{
		m.replicaConfigured,
		m.replicaHealthy,
		m.replicaLagBytes,
		m.replicaReplayLag,
		m.replicaProbes,
		m.readRoutes,
		m.readFallbacks,
	}
}

func (m *DBRoutingMetrics) SetReplicaConfigured(configured bool) {
	if m == nil {
		return
	}
	m.replicaConfigured.Set(dbBoolFloat(configured))
	if !configured {
		m.replicaHealthy.Set(0)
		m.replicaLagBytes.Set(0)
		m.replicaReplayLag.Set(0)
	}
}

func (m *DBRoutingMetrics) SetReplicaStatus(healthy bool, lagBytes int64, replayLag time.Duration) {
	if m == nil {
		return
	}
	m.replicaHealthy.Set(dbBoolFloat(healthy))
	m.replicaLagBytes.Set(float64(max(lagBytes, 0)))
	m.replicaReplayLag.Set(max(replayLag, 0).Seconds())
}

func (m *DBRoutingMetrics) ObserveReplicaProbe(healthy bool, reason string) {
	if m == nil {
		return
	}
	result := "unhealthy"
	if healthy {
		result = "healthy"
	}
	m.replicaProbes.WithLabelValues(result, reason).Inc()
}

func (m *DBRoutingMetrics) RecordReadRoute(business, role, reason string) {
	if m == nil {
		return
	}
	m.readRoutes.WithLabelValues(business, role, reason).Inc()
}

func (m *DBRoutingMetrics) RecordReadFallback(business, reason string) {
	if m == nil {
		return
	}
	m.readFallbacks.WithLabelValues(business, reason).Inc()
}

func dbBoolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
