package transportretry

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func attemptCounter(m *Metrics, provider, policyID, sessionMode, outcome string) prometheus.Counter {
	return m.attemptTotal.WithLabelValues(
		normalizeProvider(provider),
		normalizePolicy(policyID),
		normalizeSessionMode(sessionMode),
		normalizeOutcome(outcome),
	)
}

func TestMetricsSkippedToolsOutcomeIncrements(t *testing.T) {
	t.Parallel()

	m := NewMetricsForTest(nil)
	cfg := DefaultConfig()
	cfg.Policies = []Policy{cfg.Policies[0]}
	failErr := "WritableIterable is closed (result_seen=false)"
	var calls atomic.Int32
	execute := func(_ string, _ ExecOptionsView) (ResultView, int32, error) {
		calls.Add(1)
		return ResultView{Status: "failed", Error: failErr}, 1, nil
	}

	_, _, _, _, err := ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		RetryHooks{},
		MetricsObserver{Metrics: m},
		slog.Default(),
		execute,
		"prompt",
		ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if got := testutil.ToFloat64(attemptCounter(m, "cursor", "cursor_writable_iterable", "", "skipped_tools")); got != 1 {
		t.Fatalf("skipped_tools counter = %v, want 1", got)
	}
}

func TestMetricsSkippedDisabledOutcomeIncrements(t *testing.T) {
	t.Parallel()

	m := NewMetricsForTest(nil)
	cfg := Config{Enabled: false}
	var calls atomic.Int32
	execute := func(_ string, _ ExecOptionsView) (ResultView, int32, error) {
		calls.Add(1)
		return ResultView{Status: "failed", Error: "WritableIterable is closed"}, 0, nil
	}

	_, _, _, _, err := ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		RetryHooks{},
		MetricsObserver{Metrics: m},
		slog.Default(),
		execute,
		"prompt",
		ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if got := testutil.ToFloat64(attemptCounter(m, "cursor", "", "", "skipped_disabled")); got != 1 {
		t.Fatalf("skipped_disabled counter = %v, want 1", got)
	}
}

func TestExecuteWithRetryDefaultDelayBeforeFreshSession(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Policies = []Policy{cfg.Policies[0]}

	failErr := "WritableIterable is closed (result_seen=false)"
	var calls atomic.Int32
	execute := func(_ string, _ ExecOptionsView) (ResultView, int32, error) {
		calls.Add(1)
		return ResultView{Status: "failed", Error: failErr, SessionID: "sess-1"}, 0, nil
	}

	start := time.Now()
	_, _, stats, _, err := ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"sess-prior",
		RetryHooks{
			OnFreshSession: func(view *ExecOptionsView) (string, string) {
				view.ResumeSessionID = ""
				return "fresh-prompt", "sess-prior"
			},
		},
		nil,
		slog.Default(),
		execute,
		"prompt",
		ExecOptionsView{ResumeSessionID: "sess-prior"},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("calls = %d, want 4 launches before exhaustion", calls.Load())
	}
	elapsed := time.Since(start)
	if elapsed < 4900*time.Millisecond {
		t.Fatalf("elapsed = %v, want at least 5s delay before fresh_session launch", elapsed)
	}
	if got := stats.SessionModes[2]; got != SessionRetryFresh {
		t.Fatalf("session_modes[2] = %q, want fresh_session", got)
	}
}
