// Package-level note for PR4 (MUL-2947): the sampler runs at /metrics scrape
// time, hits the read replica via the existing pgxpool, and is opt-in. Every
// individual SQL statement runs in its own short read-only transaction with
// `SET LOCAL statement_timeout = '500ms'` and a hard `LIMIT 100` so a slow
// table or a hung connection cannot drag /metrics — and by extension the
// whole alerting story — down. A 5–10 second TTL cache absorbs concurrent
// scrapes from multiple Prometheus replicas.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// Default knobs. Kept conservative so the very first scrape on a busy DB
// cannot stall /metrics for more than ~half a second per query, and so a
// single scrape can never spawn a flood of identical reads even if multiple
// Prometheus replicas attach to the same pod at once.
const (
	defaultSamplerCacheTTL     = 8 * time.Second
	defaultSamplerQueryTimeout = 500 * time.Millisecond
	samplerRowLimit            = 100

	// Active-user / active-workspace windows. The label set is fixed —
	// new windows must be added explicitly here so cardinality stays
	// bounded under all input.
	windowFiveMinutes = "5m"
	windowOneHour     = "1h"
	windowOneDay      = "24h"

	// Runtime is considered online if its last_seen_at is within this
	// many seconds of `now()`. 60s matches the daemon heartbeat cadence
	// (~15s) plus headroom for redis relay lag and clock skew.
	runtimeOnlineWindowSeconds = 60

	// A running task is considered "stuck" once started_at is older
	// than this. Matches the Grafana board threshold from MUL-2328.
	stuckRunningInterval = "30 minutes"
)

// samplerWindows is the canonical list emitted on every scrape. The slice
// (rather than ranging a map) is intentional: order is stable in
// /metrics output, which keeps diffs readable and golden tests honest.
var samplerWindows = []struct {
	label string
	d     time.Duration
}{
	{windowFiveMinutes, 5 * time.Minute},
	{windowOneHour, time.Hour},
	{windowOneDay, 24 * time.Hour},
}

// BusinessSamplerOptions configures the BusinessSamplerCollector. A nil
// receiver in the registry means the sampler is disabled, which is the
// expected state for unit tests and any deployment where the operator does
// not opt in.
type BusinessSamplerOptions struct {
	// Pool is the dedicated pgxpool used by the sampler. Callers SHOULD
	// build a small pool (MaxConns 1–2) pointed at the same database as
	// the main app pool, so a sampler stall cannot starve business
	// traffic. If a caller really wants to share the main pool, they may
	// pass it here — the per-query statement_timeout still bounds the
	// blast radius.
	Pool *pgxpool.Pool

	// CacheTTL is how long a successful sample is reused before the next
	// scrape triggers a refresh. Defaults to 8s. The spec calls for
	// 5–10s; values outside that range are accepted but logged.
	CacheTTL time.Duration

	// QueryTimeout is the per-query statement_timeout pushed to Postgres
	// via SET LOCAL. Defaults to 500ms.
	QueryTimeout time.Duration
}

// samplerQuerier is the minimal pgx surface the sampler needs. Splitting it
// out lets unit tests inject a fake without spinning up a real database.
type samplerQuerier interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

// BusinessSamplerCollector implements prometheus.Collector by issuing a fixed
// set of read-only SQL queries on each scrape and exposing the results as
// gauges. See the package note above for the safety contract.
type BusinessSamplerCollector struct {
	pool         samplerQuerier
	cacheTTL     time.Duration
	queryTimeout time.Duration
	now          func() time.Time
	logger       *slog.Logger

	// refreshFn is the snapshot-producing function. Defaults to
	// refreshFromDB. Tests swap it out for a fake so they can exercise
	// caching / cardinality / emit logic without spinning up Postgres.
	refreshFn func(ctx context.Context, now time.Time) *samplerSnapshot

	// Self-introspection metrics. Registered alongside the collector via
	// Collectors() so /metrics shows query latency and error counts even
	// when the gauges themselves are stale.
	queryDuration *prometheus.HistogramVec
	queryErrors   *prometheus.CounterVec

	// Gauge descriptors. ConstMetric is preferred over a GaugeVec because
	// the values are computed once per scrape from a fresh DB read; we
	// never want stale series sticking around because a label has stopped
	// appearing in the result set.
	descActiveUsers      *prometheus.Desc
	descActiveWorkspaces *prometheus.Desc
	descTaskQueued       *prometheus.Desc
	descTaskRunning      *prometheus.Desc
	descTaskStuck        *prometheus.Desc
	descRuntimeOnline    *prometheus.Desc
	descHeartbeatAgeHist *prometheus.Desc
	descWorkspaceTotal   *prometheus.Desc

	mu       sync.Mutex
	snapshot *samplerSnapshot
}

// NewBusinessSamplerCollector builds the collector. Returns nil when opts
// is nil or has a nil Pool — that signals "sampler disabled" to the
// registry without forcing every test to provide a stub.
func NewBusinessSamplerCollector(opts *BusinessSamplerOptions) *BusinessSamplerCollector {
	if opts == nil || opts.Pool == nil {
		return nil
	}
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultSamplerCacheTTL
	}
	queryTimeout := opts.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = defaultSamplerQueryTimeout
	}
	c := &BusinessSamplerCollector{
		pool:         opts.Pool,
		cacheTTL:     cacheTTL,
		queryTimeout: queryTimeout,
		now:          time.Now,
		logger:       slog.Default(),

		queryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "multica",
			Subsystem: "business_sampler",
			Name:      "query_seconds",
			Help:      "Per-query duration of the BusinessSamplerCollector. The `name` label is one of the fixed query identifiers and never user-controlled.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"name"}),
		queryErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "business_sampler",
			Name:      "query_errors_total",
			Help:      "Per-query error count. Includes statement_timeout cancellations, which are the expected outcome of a hung database and the smoking gun for SET LOCAL working as intended.",
		}, []string{"name"}),

		descActiveUsers: prometheus.NewDesc(
			"multica_active_users",
			"Distinct users with chat / task activity in the rolling window. Sampled from the database; stale up to the sampler cache TTL.",
			[]string{"window"}, nil),
		descActiveWorkspaces: prometheus.NewDesc(
			"multica_active_workspaces",
			"Distinct workspaces with chat / task activity in the rolling window. Sampled from the database.",
			[]string{"window"}, nil),
		descTaskQueued: prometheus.NewDesc(
			"multica_agent_task_queued",
			"Current agent_task_queue rows in `queued` status by inferred source. Sampled from the database.",
			[]string{"source"}, nil),
		descTaskRunning: prometheus.NewDesc(
			"multica_agent_task_running",
			"Current agent_task_queue rows in `dispatched` or `running` status by inferred source and runtime mode. Sampled from the database.",
			[]string{"source", "runtime_mode"}, nil),
		descTaskStuck: prometheus.NewDesc(
			"multica_agent_task_stuck_total",
			"Current `running` agent_task_queue rows whose started_at is older than the stuck threshold. Sampled from the database.",
			[]string{"source"}, nil),
		descRuntimeOnline: prometheus.NewDesc(
			"multica_runtime_online",
			"Count of agent_runtime rows with last_seen_at within the online heartbeat window. Sampled from the database.",
			[]string{"runtime_mode", "provider"}, nil),
		descHeartbeatAgeHist: prometheus.NewDesc(
			"multica_runtime_heartbeat_age_seconds",
			"Distribution of (now() - agent_runtime.last_seen_at) for runtimes considered online by the sampler.",
			[]string{"runtime_mode"}, nil),
		descWorkspaceTotal: prometheus.NewDesc(
			"multica_workspace_total",
			"Lifetime workspace row count. Useful for sizing alerts and dashboards.",
			nil, nil),
	}
	c.refreshFn = c.refreshFromDB
	return c
}

// Collectors returns every prometheus.Collector that the registry must mount
// to expose this sampler — the gauges (the receiver itself) plus the query
// duration histogram and error counter.
func (c *BusinessSamplerCollector) Collectors() []prometheus.Collector {
	if c == nil {
		return nil
	}
	return []prometheus.Collector{c, c.queryDuration, c.queryErrors}
}

// Describe implements prometheus.Collector.
func (c *BusinessSamplerCollector) Describe(ch chan<- *prometheus.Desc) {
	if c == nil {
		return
	}
	for _, d := range []*prometheus.Desc{
		c.descActiveUsers,
		c.descActiveWorkspaces,
		c.descTaskQueued,
		c.descTaskRunning,
		c.descTaskStuck,
		c.descRuntimeOnline,
		c.descHeartbeatAgeHist,
		c.descWorkspaceTotal,
	} {
		ch <- d
	}
}

// Collect implements prometheus.Collector. It returns the cached snapshot
// when fresh; otherwise it triggers a refresh under the mutex so concurrent
// scrapes share one DB round-trip. A refresh failure is logged and the last
// known snapshot is reused — this is the "metric briefly stale, sampler
// does not crash" behavior the PR4 spec requires under DB hangs.
func (c *BusinessSamplerCollector) Collect(ch chan<- prometheus.Metric) {
	if c == nil || c.pool == nil {
		return
	}
	snap := c.maybeRefresh()
	if snap == nil {
		return
	}
	c.emit(ch, snap)
}

func (c *BusinessSamplerCollector) maybeRefresh() *samplerSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.snapshot != nil && now.Sub(c.snapshot.takenAt) < c.cacheTTL {
		return c.snapshot
	}

	// Bound the entire refresh to N×queryTimeout so an in-flight scrape
	// can never block forever even if SET LOCAL is somehow ignored by a
	// misconfigured Postgres.
	ctx, cancel := context.WithTimeout(context.Background(), 8*c.queryTimeout)
	defer cancel()

	next := c.refreshFn(ctx, now)
	if next != nil {
		c.snapshot = next
	}
	return c.snapshot
}

// emit walks a snapshot and writes one prometheus.Metric per (desc, labels)
// pair. Pure data-shaping; no DB or locking.
func (c *BusinessSamplerCollector) emit(ch chan<- prometheus.Metric, snap *samplerSnapshot) {
	for _, w := range samplerWindows {
		ch <- prometheus.MustNewConstMetric(
			c.descActiveUsers, prometheus.GaugeValue, snap.activeUsers[w.label], w.label)
		ch <- prometheus.MustNewConstMetric(
			c.descActiveWorkspaces, prometheus.GaugeValue, snap.activeWorkspaces[w.label], w.label)
	}

	// Always emit at least one zero-valued series for queued/running per
	// known source so dashboards don't show "no data" right after a
	// process restart. Real values overwrite the zero; missing sources
	// stay at zero.
	for _, source := range knownSourceLabels() {
		ch <- prometheus.MustNewConstMetric(
			c.descTaskQueued, prometheus.GaugeValue, snap.taskQueued[source], source)
		ch <- prometheus.MustNewConstMetric(
			c.descTaskStuck, prometheus.GaugeValue, snap.taskStuck[source], source)
	}
	for _, source := range knownSourceLabels() {
		for _, mode := range knownRuntimeModeLabels() {
			key := taskRunningKey{source: source, runtimeMode: mode}
			ch <- prometheus.MustNewConstMetric(
				c.descTaskRunning, prometheus.GaugeValue, snap.taskRunning[key], source, mode)
		}
	}

	for key, val := range snap.runtimeOnline {
		ch <- prometheus.MustNewConstMetric(
			c.descRuntimeOnline, prometheus.GaugeValue, val, key.runtimeMode, key.provider)
	}

	for mode, hist := range snap.heartbeatAge {
		ch <- prometheus.MustNewConstHistogram(
			c.descHeartbeatAgeHist,
			hist.count,
			hist.sum,
			hist.buckets,
			mode,
		)
	}

	if snap.workspaceTotalKnown {
		ch <- prometheus.MustNewConstMetric(
			c.descWorkspaceTotal, prometheus.GaugeValue, snap.workspaceTotal)
	}
}

// knownSourceLabels enumerates the source values we always emit a zero for.
// Pulled from the existing BusinessMetrics whitelist so the sampler never
// invents a new bucket.
func knownSourceLabels() []string {
	return []string{"chat", "issue", "autopilot", "autopilot_issue", "quick_create", "manual", "api", "other"}
}

func knownRuntimeModeLabels() []string {
	return []string{"local", "cloud", "unknown"}
}

// heartbeatAgeBuckets matches the Grafana board's runtime-health view: a few
// seconds for healthy heartbeats, then quickly out to "definitely stale".
var heartbeatAgeBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600}

// taskRunningKey is the composite gauge label key for the running counter.
// Defined here because it is shared between the snapshot and the emit path.
type taskRunningKey struct {
	source      string
	runtimeMode string
}

type runtimeOnlineKey struct {
	runtimeMode string
	provider    string
}

// samplerHistogram is the in-memory representation of a single
// prometheus.ConstHistogram for one runtime_mode. We bucketise in Go
// because Postgres does not return histogram-shaped data directly.
type samplerHistogram struct {
	count   uint64
	sum     float64
	buckets map[float64]uint64
}

// samplerSnapshot is the cached output of one full refresh. All maps are
// pre-allocated so the emit path can read them lock-free once the receiver
// has handed it back.
type samplerSnapshot struct {
	takenAt time.Time

	activeUsers      map[string]float64
	activeWorkspaces map[string]float64

	taskQueued  map[string]float64
	taskRunning map[taskRunningKey]float64
	taskStuck   map[string]float64

	runtimeOnline map[runtimeOnlineKey]float64
	heartbeatAge  map[string]samplerHistogram

	workspaceTotal      float64
	workspaceTotalKnown bool
}

func newSamplerSnapshot(t time.Time) *samplerSnapshot {
	return &samplerSnapshot{
		takenAt:          t,
		activeUsers:      map[string]float64{},
		activeWorkspaces: map[string]float64{},
		taskQueued:       map[string]float64{},
		taskRunning:      map[taskRunningKey]float64{},
		taskStuck:        map[string]float64{},
		runtimeOnline:    map[runtimeOnlineKey]float64{},
		heartbeatAge:     map[string]samplerHistogram{},
	}
}

// refreshFromDB runs every sampler query in sequence on a single acquired
// connection. Each query is wrapped in its own short read-only transaction
// with SET LOCAL statement_timeout, so a hang on one statement cannot
// poison the others. Errors are logged-and-continued: a partial snapshot is
// strictly better than a missing one.
func (c *BusinessSamplerCollector) refreshFromDB(ctx context.Context, now time.Time) *samplerSnapshot {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		c.queryErrors.WithLabelValues("acquire").Inc()
		c.logger.Warn("business sampler: acquire connection failed", "error", err)
		// Reuse the last snapshot if any; mark nothing fresh.
		return c.snapshot
	}
	defer conn.Release()

	snap := newSamplerSnapshot(now)

	c.runQuery(ctx, conn, "active_users", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryActiveUsers(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "active_workspaces", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryActiveWorkspaces(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_queued", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskQueued(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_running", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskRunning(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_stuck", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskStuck(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "runtime_online", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryRuntimeOnline(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "runtime_heartbeat_age", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryRuntimeHeartbeatAge(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "workspace_total", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryWorkspaceTotal(ctx, tx, snap)
	})

	return snap
}

// runQuery wraps one logical sampler query: BEGIN READ ONLY, SET LOCAL
// statement_timeout, run, COMMIT. Failures are recorded on the error
// counter and the query duration histogram, but never propagate; the
// snapshot is left with whatever default value (typically 0) the caller
// pre-seeded.
func (c *BusinessSamplerCollector) runQuery(
	ctx context.Context,
	conn *pgxpool.Conn,
	name string,
	body func(ctx context.Context, tx pgx.Tx) error,
) {
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout+50*time.Millisecond)
	defer cancel()

	start := c.now()
	defer func() {
		c.queryDuration.WithLabelValues(name).Observe(c.now().Sub(start).Seconds())
	}()

	tx, err := conn.BeginTx(queryCtx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
		IsoLevel:   pgx.ReadCommitted,
	})
	if err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: begin tx failed", "name", name, "error", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			// Best-effort rollback; the connection is already going
			// back to the pool in the caller's defer.
			_ = tx.Rollback(context.Background())
		}
	}()

	// SET LOCAL is the only knob we trust: it is automatically scoped to
	// the surrounding transaction even if the connection is later reused
	// by another caller.
	timeoutMs := int(c.queryTimeout / time.Millisecond)
	if _, err := tx.Exec(queryCtx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: SET LOCAL statement_timeout failed", "name", name, "error", err)
		return
	}

	if err := body(queryCtx, tx); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		// Statement timeouts surface as pgx errors with code 57014; we
		// log them at INFO because they are an expected steady-state
		// outcome on a degraded DB, not a bug to alert on.
		level := slog.LevelWarn
		if isStatementTimeout(err) {
			level = slog.LevelInfo
		}
		c.logger.Log(ctx, level, "business sampler: query failed", "name", name, "error", err)
		return
	}

	if err := tx.Commit(queryCtx); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: commit failed", "name", name, "error", err)
		return
	}
	committed = true
}

// isStatementTimeout returns true when err looks like the canonical
// statement_timeout cancellation. Used purely to choose a log level.
func isStatementTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// pgx surfaces SQLSTATE 57014 ("query_canceled") on statement_timeout.
	// We don't import the pgconn type just to type-assert here; a
	// substring check is good enough for a log-level decision.
	msg := err.Error()
	return contains(msg, "57014") || contains(msg, "canceling statement due to statement timeout")
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
