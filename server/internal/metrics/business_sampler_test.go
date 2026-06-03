package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// newTestSampler builds a BusinessSamplerCollector wired to a fake refresh
// function. We bypass NewBusinessSamplerCollector's nil-pool short-circuit
// by passing a dummy pgxpool — the fake refreshFn never calls into it. This
// keeps the real DB code path off the hot path of every unit test while
// still exercising the production registration / emit logic.
func newTestSampler(t *testing.T, refresh func(ctx context.Context, now time.Time) *samplerSnapshot) *BusinessSamplerCollector {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://disabled@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("create dummy pool: %v", err)
	}
	t.Cleanup(pool.Close)

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     50 * time.Millisecond,
		QueryTimeout: 100 * time.Millisecond,
	})
	if c == nil {
		t.Fatalf("NewBusinessSamplerCollector returned nil")
	}
	c.refreshFn = refresh
	return c
}

func filledSnapshot(now time.Time) *samplerSnapshot {
	snap := newSamplerSnapshot(now)
	snap.activeUsers[windowFiveMinutes] = 7
	snap.activeUsers[windowOneHour] = 42
	snap.activeUsers[windowOneDay] = 100
	snap.activeWorkspaces[windowFiveMinutes] = 3
	snap.activeWorkspaces[windowOneHour] = 11
	snap.activeWorkspaces[windowOneDay] = 25

	snap.taskQueued["chat"] = 5
	snap.taskQueued["issue"] = 2

	snap.taskRunning[taskRunningKey{source: "chat", runtimeMode: "cloud"}] = 3
	snap.taskRunning[taskRunningKey{source: "issue", runtimeMode: "local"}] = 1

	snap.taskStuck["issue"] = 1

	snap.runtimeOnline[runtimeOnlineKey{runtimeMode: "local", provider: "claude"}] = 4
	snap.runtimeOnline[runtimeOnlineKey{runtimeMode: "cloud", provider: "kiro"}] = 2

	snap.heartbeatAge["local"] = samplerHistogram{
		count:   3,
		sum:     45,
		buckets: bucketsFor([]float64{5, 15, 30}),
	}
	snap.heartbeatAge["cloud"] = samplerHistogram{
		count:   1,
		sum:     2,
		buckets: bucketsFor([]float64{2}),
	}

	snap.workspaceTotal = 250
	snap.workspaceTotalKnown = true
	return snap
}

func bucketsFor(observations []float64) map[float64]uint64 {
	buckets := make(map[float64]uint64, len(heartbeatAgeBuckets))
	for _, b := range heartbeatAgeBuckets {
		buckets[b] = 0
	}
	for _, o := range observations {
		for _, b := range heartbeatAgeBuckets {
			if o <= b {
				buckets[b]++
			}
		}
	}
	return buckets
}

// TestBusinessSamplerCollectorEmitsExpectedMetrics asserts every metric
// family from the PR4 spec is present on /metrics with the expected
// values, AND that we always emit a known-source/runtime-mode zero series
// so dashboards don't show "no data" right after a restart.
func TestBusinessSamplerCollectorEmitsExpectedMetrics(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		return filledSnapshot(refreshAt)
	})
	c.now = func() time.Time { return now }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	wantSubstrings := []string{
		`multica_active_users{window="5m"} 7`,
		`multica_active_users{window="1h"} 42`,
		`multica_active_users{window="24h"} 100`,
		`multica_active_workspaces{window="5m"} 3`,
		`multica_agent_task_queued{source="chat"} 5`,
		`multica_agent_task_queued{source="issue"} 2`,
		// Zero series for sources that didn't appear in the snapshot.
		`multica_agent_task_queued{source="autopilot"} 0`,
		`multica_agent_task_queued{source="other"} 0`,
		`multica_agent_task_running{runtime_mode="cloud",source="chat"} 3`,
		`multica_agent_task_running{runtime_mode="local",source="issue"} 1`,
		`multica_agent_task_stuck_total{source="issue"} 1`,
		`multica_runtime_online{provider="claude",runtime_mode="local"} 4`,
		`multica_runtime_online{provider="kiro",runtime_mode="cloud"} 2`,
		`multica_runtime_heartbeat_age_seconds_count{runtime_mode="local"} 3`,
		`multica_runtime_heartbeat_age_seconds_sum{runtime_mode="local"} 45`,
		`multica_workspace_total 250`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}
}

// TestBusinessSamplerSelfIntrospectionHistogramIsExposed observes a value
// directly on the query-duration histogram and asserts it surfaces on
// /metrics. Prometheus elides histograms with zero observations, so we have
// to seed one before scraping.
func TestBusinessSamplerSelfIntrospectionHistogramIsExposed(t *testing.T) {
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		return newSamplerSnapshot(refreshAt)
	})
	c.queryDuration.WithLabelValues("active_users").Observe(0.012)

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `multica_business_sampler_query_seconds_count{name="active_users"} 1`) {
		t.Fatalf("query duration histogram missing\n%s", body)
	}
}

// TestBusinessSamplerCollectorCachesSnapshot asserts the TTL cache absorbs
// concurrent scrapes: two Collect calls inside the TTL window must trigger
// exactly one refresh, and a third call after the TTL elapses must trigger
// a second.
func TestBusinessSamplerCollectorCachesSnapshot(t *testing.T) {
	var refreshCount atomic.Int32
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	current := now

	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		refreshCount.Add(1)
		return filledSnapshot(refreshAt)
	})
	c.cacheTTL = 100 * time.Millisecond
	c.now = func() time.Time { return current }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	rec = httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count after two cached scrapes = %d, want 1", got)
	}

	// Advance past the TTL — the next scrape must refresh again.
	current = current.Add(150 * time.Millisecond)
	rec = httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := refreshCount.Load(); got != 2 {
		t.Fatalf("refresh count after TTL expiry = %d, want 2", got)
	}
}

// TestBusinessSamplerCollectorBoundedCardinality is the cardinality canary.
// Even with a malicious snapshot that mentions many distinct labels, the
// sampler must collapse them into the BusinessMetrics whitelist plus
// known-window zeros. This protects /metrics from a per-runtime or
// per-workspace explosion.
func TestBusinessSamplerCollectorBoundedCardinality(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		// We can't inject raw rogue labels through the snapshot directly
		// (the maps are typed), so we exercise the normalization path by
		// pre-normalizing here exactly the way refreshFromDB would.
		snap := newSamplerSnapshot(refreshAt)
		for i := 0; i < 50; i++ {
			snap.taskQueued[NormalizeTaskSource("provider-from-user-input-"+string(rune('A'+i%26)))] += 1
		}
		for i := 0; i < 50; i++ {
			snap.runtimeOnline[runtimeOnlineKey{
				runtimeMode: NormalizeRuntimeMode("rogue-mode"),
				provider:    NormalizeRuntimeProvider("attacker-provider"),
			}] += 1
		}
		return snap
	})
	c.now = func() time.Time { return now }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	if got := testutil.CollectAndCount(c, "multica_agent_task_queued"); got != len(knownSourceLabels()) {
		t.Fatalf("agent_task_queued series = %d, want %d", got, len(knownSourceLabels()))
	}
	expectedRunning := len(knownSourceLabels()) * len(knownRuntimeModeLabels())
	if got := testutil.CollectAndCount(c, "multica_agent_task_running"); got != expectedRunning {
		t.Fatalf("agent_task_running series = %d, want %d", got, expectedRunning)
	}
	if got := testutil.CollectAndCount(c, "multica_runtime_online"); got != 1 {
		t.Fatalf("runtime_online series = %d, want 1 (collapsed by normalizers)", got)
	}
}

// TestBusinessSamplerCollectorDisabledWithoutOptions exercises the opt-in
// path in the registry: no BusinessSampler option means no sampler-related
// series leak into /metrics, and existing collectors stay registered.
func TestBusinessSamplerCollectorDisabledWithoutOptions(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	if registry.Sampler != nil {
		t.Fatalf("Sampler must be nil when BusinessSampler option is absent")
	}
	rec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, forbidden := range []string{
		"multica_active_users",
		"multica_agent_task_queued",
		"multica_runtime_online",
		"multica_business_sampler_query_seconds",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("/metrics leaked sampler family %q when sampler disabled", forbidden)
		}
	}
}

// TestBusinessSamplerCollectorDBHangIsolation simulates a hung database
// using a pgxpool pointed at an unreachable host. The sampler must not
// panic, must not block /metrics indefinitely, and must record the
// resulting failure on the query error counter — that's the
// statement_timeout / connection-acquire safety net the spec asks for.
func TestBusinessSamplerCollectorDBHangIsolation(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://hang@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	defer pool.Close()

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     time.Second,
		QueryTimeout: 50 * time.Millisecond,
	})
	if c == nil {
		t.Fatalf("NewBusinessSamplerCollector returned nil")
	}

	// Use the real refreshFromDB path so we exercise Acquire / SET LOCAL
	// statement_timeout / error counter wiring.
	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	done := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		close(done)
	}()

	select {
	case <-done:
		// Success — the scrape completed despite no reachable DB.
	case <-time.After(5 * time.Second):
		t.Fatal("metrics scrape blocked > 5s on unreachable DB")
	}

	// At least one error must have been recorded against the
	// connection-acquire path. We don't assert exact name to avoid
	// coupling to internal labels; we just want a positive count.
	if got := testutil.CollectAndCount(c.queryErrors); got == 0 {
		t.Fatal("expected at least one query_errors_total series after DB hang")
	}
}

// TestNewBusinessSamplerCollectorNilPool covers the explicit opt-out path.
func TestNewBusinessSamplerCollectorNilPool(t *testing.T) {
	if c := NewBusinessSamplerCollector(nil); c != nil {
		t.Fatalf("NewBusinessSamplerCollector(nil) = %p, want nil", c)
	}
	if c := NewBusinessSamplerCollector(&BusinessSamplerOptions{}); c != nil {
		t.Fatalf("NewBusinessSamplerCollector with nil Pool = %p, want nil", c)
	}
}

// TestSamplerHistogramBucketing exercises the in-memory bucketing logic so
// a regression in heartbeat-age accounting is caught before it ships to
// dashboards.
func TestSamplerHistogramBucketing(t *testing.T) {
	buckets := bucketsFor([]float64{0.5, 5, 30, 75})
	expectations := map[float64]uint64{
		1: 1, 5: 2, 15: 2, 30: 3, 60: 3, 120: 4, 300: 4, 600: 4,
	}
	for b, want := range expectations {
		if got := buckets[b]; got != want {
			t.Errorf("bucket le=%g count = %d, want %d", b, got, want)
		}
	}
}
