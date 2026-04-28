package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type DBCollector struct {
	pool *pgxpool.Pool

	acquiredConns         *prometheus.Desc
	idleConns             *prometheus.Desc
	maxConns              *prometheus.Desc
	totalConns            *prometheus.Desc
	constructingConns     *prometheus.Desc
	acquireCount          *prometheus.Desc
	acquireDuration       *prometheus.Desc
	emptyAcquireCount     *prometheus.Desc
	emptyAcquireWaitTime  *prometheus.Desc
	canceledAcquireCount  *prometheus.Desc
	newConnsCount         *prometheus.Desc
	maxIdleDestroyCount   *prometheus.Desc
	maxLifetimeDestroyCnt *prometheus.Desc
}

func NewDBCollector(pool *pgxpool.Pool) *DBCollector {
	return &DBCollector{
		pool: pool,

		acquiredConns:         newDBDesc("acquired_conns", "Currently acquired PostgreSQL connections."),
		idleConns:             newDBDesc("idle_conns", "Currently idle PostgreSQL connections."),
		maxConns:              newDBDesc("max_conns", "Maximum PostgreSQL connections allowed by the pool."),
		totalConns:            newDBDesc("total_conns", "Total PostgreSQL connections currently in the pool."),
		constructingConns:     newDBDesc("constructing_conns", "PostgreSQL connections currently being established."),
		acquireCount:          newDBDesc("acquire_count", "Total successful PostgreSQL connection acquires."),
		acquireDuration:       newDBDesc("acquire_duration_seconds_total", "Total time spent acquiring PostgreSQL connections."),
		emptyAcquireCount:     newDBDesc("empty_acquire_count", "Total acquires that waited because the PostgreSQL pool was empty."),
		emptyAcquireWaitTime:  newDBDesc("empty_acquire_wait_seconds_total", "Total time spent waiting for PostgreSQL connections when the pool was empty."),
		canceledAcquireCount:  newDBDesc("canceled_acquire_count", "Total canceled PostgreSQL connection acquires."),
		newConnsCount:         newDBDesc("new_conns_count", "Total PostgreSQL connections created by the pool."),
		maxIdleDestroyCount:   newDBDesc("max_idle_destroy_count", "Total PostgreSQL connections destroyed due to idle limits."),
		maxLifetimeDestroyCnt: newDBDesc("max_lifetime_destroy_count", "Total PostgreSQL connections destroyed due to max lifetime."),
	}
}

func newDBDesc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc("multica_db_pool_"+name, help, nil, nil)
}

func (c *DBCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.acquiredConns,
		c.idleConns,
		c.maxConns,
		c.totalConns,
		c.constructingConns,
		c.acquireCount,
		c.acquireDuration,
		c.emptyAcquireCount,
		c.emptyAcquireWaitTime,
		c.canceledAcquireCount,
		c.newConnsCount,
		c.maxIdleDestroyCount,
		c.maxLifetimeDestroyCnt,
	} {
		ch <- desc
	}
}

func (c *DBCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool == nil {
		return
	}
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stat.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stat.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stat.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.constructingConns, prometheus.GaugeValue, float64(stat.ConstructingConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(stat.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, stat.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireCount, prometheus.CounterValue, float64(stat.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireWaitTime, prometheus.CounterValue, stat.EmptyAcquireWaitTime().Seconds())
	ch <- prometheus.MustNewConstMetric(c.canceledAcquireCount, prometheus.CounterValue, float64(stat.CanceledAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.newConnsCount, prometheus.CounterValue, float64(stat.NewConnsCount()))
	ch <- prometheus.MustNewConstMetric(c.maxIdleDestroyCount, prometheus.CounterValue, float64(stat.MaxIdleDestroyCount()))
	ch <- prometheus.MustNewConstMetric(c.maxLifetimeDestroyCnt, prometheus.CounterValue, float64(stat.MaxLifetimeDestroyCount()))
}
