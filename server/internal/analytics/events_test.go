package analytics

import "testing"

func TestRuntimeReadyOmitsUnmeasuredDuration(t *testing.T) {
	ev := RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 0)
	if _, ok := ev.Properties["ready_duration_ms"]; ok {
		t.Fatalf("ready_duration_ms should be omitted until it is measured")
	}

	ev = RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 123)
	if got := ev.Properties["ready_duration_ms"]; got != int64(123) {
		t.Fatalf("ready_duration_ms = %v, want 123", got)
	}
}

func TestFailedEventsUseWillRetry(t *testing.T) {
	runEv := AutopilotRunFailed("user-1", "workspace-1", "autopilot-1", "run-1", "manual", AutopilotAssignee{AgentID: "agent-1", AssigneeType: "agent"}, "manual", "task failed", "task_error", false, 10)
	if got := runEv.Properties["will_retry"]; got != false {
		t.Fatalf("autopilot will_retry = %v, want false", got)
	}
	if _, ok := runEv.Properties["recoverable"]; ok {
		t.Fatalf("autopilot failure should not emit recoverable")
	}
}

func TestIsMetricsOnly(t *testing.T) {
	// As of MUL-4127, PostHog is retired for server-side product analytics:
	// every server-side event is Prometheus-only and must not ship to PostHog.
	for _, name := range []string{
		// runtime / autopilot execution-lifecycle telemetry
		EventRuntimeRegistered, EventRuntimeReady, EventRuntimeFailed, EventRuntimeOffline,
		EventAutopilotRunStarted, EventAutopilotRunCompleted, EventAutopilotRunFailed,
		// product-behaviour events (now DB + Grafana only)
		EventSignup, EventWorkspaceCreated, EventIssueCreated, EventIssueExecuted,
		EventChatMessageSent, EventTeamInviteSent, EventTeamInviteAccepted,
		EventOnboardingStarted, EventOnboardingQuestionnaireSubmit, EventAgentCreated,
		EventOnboardingCompleted, EventCloudWaitlistJoined, EventFeedbackSubmitted,
		EventContactSalesSubmitted, EventSquadCreated, EventAutopilotCreated,
	} {
		if !IsMetricsOnly(name) {
			t.Errorf("IsMetricsOnly(%q) = false, want true (server events stay out of PostHog since MUL-4127)", name)
		}
	}
	// A name that isn't a declared server event is not metrics-only.
	if IsMetricsOnly("$exception") {
		t.Errorf("IsMetricsOnly(%q) = true, want false (frontend-only event)", "$exception")
	}
}
