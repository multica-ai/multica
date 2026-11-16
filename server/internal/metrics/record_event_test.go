package metrics_test

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/metrics"
)

// captureSpy records the names of every event handed to Capture so tests can
// assert which events actually reach PostHog.
type captureSpy struct{ names []string }

func (c *captureSpy) Capture(e analytics.Event) { c.names = append(c.names, e.Name) }
func (c *captureSpy) Close()                    {}

// TestRecordEventSkipsPostHogForMetricsOnly verifies that events flagged by
// analytics.IsMetricsOnly increment a Prometheus counter but are NOT shipped to
// PostHog. As of MUL-4127 every server-side event is metrics-only, so none of
// them reach PostHog; only a name outside the set (a frontend event) would.
func TestRecordEventSkipsPostHogForMetricsOnly(t *testing.T) {
	spy := &captureSpy{}
	m := metrics.NewBusinessMetrics()

	// Operational event: Prometheus counter moves, PostHog gets nothing.
	before := metrics.SumAllCounters(m)
	metrics.RecordEvent(spy, m, analytics.RuntimeOffline("user-1", "ws-1", "rt-1", "daemon-1", "claude"))
	if len(spy.names) != 0 {
		t.Fatalf("runtime_offline shipped %d events to PostHog, want 0: %v", len(spy.names), spy.names)
	}
	if metrics.SumAllCounters(m) <= before {
		t.Fatalf("runtime_offline did not increment a Prometheus counter")
	}

	// Product-behaviour event: since MUL-4127 it is metrics-only too — the
	// Prometheus counter still moves but nothing ships to PostHog.
	before = metrics.SumAllCounters(m)
	metrics.RecordEvent(spy, m, analytics.WorkspaceCreated("user-1", "ws-1"))
	if len(spy.names) != 0 {
		t.Fatalf("workspace_created shipped to PostHog, want 0 (metrics-only since MUL-4127): %v", spy.names)
	}
	if metrics.SumAllCounters(m) <= before {
		t.Fatalf("workspace_created did not increment a Prometheus counter")
	}

	// Sanity-check the Capture path is still wired: a name outside the
	// metrics-only set (i.e. a frontend event) is shipped to PostHog.
	metrics.RecordEvent(spy, m, analytics.Event{Name: "frontend_only_probe", DistinctID: "user-1"})
	if len(spy.names) != 1 || spy.names[0] != "frontend_only_probe" {
		t.Fatalf("a non-metrics-only event should ship to PostHog exactly once: %v", spy.names)
	}
}
