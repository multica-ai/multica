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
		EventOnboardingStarted, EventOnboardingQuestionnaireSubmit, EventOnboardingSourceSubmit,
		EventAgentCreated,
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

func TestOnboardingSourceSubmittedSetOnlyWhenAnswered(t *testing.T) {
	answered := OnboardingSourceSubmitted("u1", []string{"search"}, false, false)
	if answered.Properties["source_skipped"] != false {
		t.Fatalf("answered: source_skipped = %v, want false", answered.Properties["source_skipped"])
	}
	if answered.Set == nil || answered.Set["source"] == nil {
		t.Fatalf("answered: expected $set source, got %v", answered.Set)
	}

	declined := OnboardingSourceSubmitted("u1", nil, true, false)
	if declined.Properties["source_skipped"] != true {
		t.Fatalf("declined: source_skipped = %v, want true", declined.Properties["source_skipped"])
	}
	if declined.Set != nil {
		t.Fatalf("declined: a skip has nothing to mirror — expected nil Set, got %v", declined.Set)
	}
	// nil slice must normalize to [] so property types stay stable.
	// (Key is acquisition_source — plain "source" is the event-source
	// dimension stamped by core properties.)
	if src, ok := declined.Properties["acquisition_source"].([]string); !ok || src == nil {
		t.Fatalf("declined: acquisition_source property = %#v, want empty []string", declined.Properties["acquisition_source"])
	}
}
