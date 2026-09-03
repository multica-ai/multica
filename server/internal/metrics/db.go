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

		acquiredConns:         newDBDesc("acquired_conns", "PostgreSQL pool connections currently acquired, by database role."),
		idleConns:             newDBDesc("idle_conns", "PostgreSQL pool connections currently idle, by database role."),
		maxConns:              newDBDesc("max_conns", "Maximum PostgreSQL pool connections by database role."),
		totalConns:            newDBDesc("total_conns", "Total current PostgreSQL pool connections by database role."),
		constructingConns:     newDBDesc("constructing_conns", "PostgreSQL pool connections currently being established by database role."),
		acquireCount:          newDBDesc("acquire_count", "Total successful PostgreSQL pool acquires by database role."),
		acquireDuration:       newDBDesc("acquire_duration_seconds_total", "Total time spent acquiring PostgreSQL pool connections by database role."),
		emptyAcquireCount:     newDBDesc("empty_acquire_count", "Total acquires that waited on an empty PostgreSQL pool by database role."),
		emptyAcquireWaitTime:  newDBDesc("empty_acquire_wait_seconds_total", "Total time spent waiting on an empty PostgreSQL pool by database role."),
		canceledAcquireCount:  newDBDesc("canceled_acquire_count", "Total canceled PostgreSQL pool acquires by database role."),
		newConnsCount:         newDBDesc("new_conns_count", "Total PostgreSQL pool connections created by database role."),
		maxIdleDestroyCount:   newDBDesc("max_idle_destroy_count", "Total PostgreSQL pool connections destroyed due to idle limits by database role."),
		maxLifetimeDestroyCnt: newDBDesc("max_lifetime_destroy_count", "Total PostgreSQL pool connections destroyed due to max lifetime by database role."),
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
