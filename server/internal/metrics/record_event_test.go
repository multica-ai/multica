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

// TestRecordEventSkipsPostHogForMetricsOnly verifies that operational events
// flagged by analytics.IsMetricsOnly still increment a Prometheus counter but
// are NOT shipped to PostHog, while product-behaviour events are shipped.
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

	metrics.RecordEvent(spy, m, analytics.AutopilotRunCompleted("user-1", "ws-1", "ap-1", "run-1", "manual", analytics.AutopilotAssignee{AgentID: "agent-1", AssigneeType: "agent"}, "manual", 10))
	if len(spy.names) != 0 {
		t.Fatalf("autopilot_run_completed shipped to PostHog, want 0: %v", spy.names)
	}

	// Product-behaviour event: still shipped to PostHog.
	metrics.RecordEvent(spy, m, analytics.WorkspaceCreated("user-1", "ws-1"))
	if len(spy.names) != 1 || spy.names[0] != analytics.EventWorkspaceCreated {
		t.Fatalf("workspace_created was not shipped to PostHog exactly once: %v", spy.names)
	}
}
