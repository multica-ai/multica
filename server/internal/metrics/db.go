package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type DBCollector struct {
	pools []namedDBPool

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

type namedDBPool struct {
	role string
	pool *pgxpool.Pool
}

func NewDBCollector(primary, replica *pgxpool.Pool) *DBCollector {
	pools := make([]namedDBPool, 0, 2)
	if primary != nil {
		pools = append(pools, namedDBPool{role: "primary", pool: primary})
	}
	if replica != nil {
		pools = append(pools, namedDBPool{role: "replica", pool: replica})
	}
	return &DBCollector{
		pools: pools,

		acquiredConns:         newDBDesc("acquired_conns", "Number of acquired connections in each PostgreSQL pool."),
		idleConns:             newDBDesc("idle_conns", "Number of idle connections in each PostgreSQL pool."),
		maxConns:              newDBDesc("max_conns", "Maximum connections configured for each PostgreSQL pool."),
		totalConns:            newDBDesc("total_conns", "Number of current connections in each PostgreSQL pool."),
		constructingConns:     newDBDesc("constructing_conns", "Number of connections being established in each PostgreSQL pool."),
		acquireCount:          newDBDesc("acquire_count", "Total successful connection acquisitions from each PostgreSQL pool."),
		acquireDuration:       newDBDesc("acquire_duration_seconds_total", "Total time spent acquiring connections from each PostgreSQL pool."),
		emptyAcquireCount:     newDBDesc("empty_acquire_count", "Total acquisitions that waited for an available connection in each PostgreSQL pool."),
		emptyAcquireWaitTime:  newDBDesc("empty_acquire_wait_seconds_total", "Total time spent waiting for an available connection in each PostgreSQL pool."),
		canceledAcquireCount:  newDBDesc("canceled_acquire_count", "Total canceled connection acquisitions from each PostgreSQL pool."),
		newConnsCount:         newDBDesc("new_conns_count", "Total connections created in each PostgreSQL pool."),
		maxIdleDestroyCount:   newDBDesc("max_idle_destroy_count", "Total connections destroyed by each PostgreSQL pool after exceeding the idle limit."),
		maxLifetimeDestroyCnt: newDBDesc("max_lifetime_destroy_count", "Total connections destroyed by each PostgreSQL pool after exceeding the maximum lifetime."),
	}
}

func newDBDesc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc("multica_db_pool_"+name, help, []string{"role"}, nil)
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
	for _, namedPool := range c.pools {
		collectDBPool(ch, namedPool.role, namedPool.pool.Stat(), c)
	}
}

func collectDBPool(ch chan<- prometheus.Metric, role string, stat *pgxpool.Stat, c *DBCollector) {
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(stat.AcquiredConns()), role)
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(stat.IdleConns()), role)
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()), role)
	ch <- prometheus.MustNewConstMetric(c.totalConns, prometheus.GaugeValue, float64(stat.TotalConns()), role)
	ch <- prometheus.MustNewConstMetric(c.constructingConns, prometheus.GaugeValue, float64(stat.ConstructingConns()), role)
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(stat.AcquireCount()), role)
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, stat.AcquireDuration().Seconds(), role)
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireCount, prometheus.CounterValue, float64(stat.EmptyAcquireCount()), role)
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireWaitTime, prometheus.CounterValue, stat.EmptyAcquireWaitTime().Seconds(), role)
	ch <- prometheus.MustNewConstMetric(c.canceledAcquireCount, prometheus.CounterValue, float64(stat.CanceledAcquireCount()), role)
	ch <- prometheus.MustNewConstMetric(c.newConnsCount, prometheus.CounterValue, float64(stat.NewConnsCount()), role)
	ch <- prometheus.MustNewConstMetric(c.maxIdleDestroyCount, prometheus.CounterValue, float64(stat.MaxIdleDestroyCount()), role)
	ch <- prometheus.MustNewConstMetric(c.maxLifetimeDestroyCnt, prometheus.CounterValue, float64(stat.MaxLifetimeDestroyCount()), role)
}
