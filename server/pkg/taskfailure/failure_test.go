package taskfailure

import (
	"strings"
	"testing"
)

// TestReasonStringWireValues pins the on-the-wire string for every
// canonical reason. These strings are persisted into
// agent_task_queue.failure_reason and surfaced as Prometheus labels —
// renaming any of them is a breaking change. If this test fails because
// you intended to rename a value, also update the SQL classifier in
// MUL-1949 and ship a backfill migration before changing the constant.
func TestReasonStringWireValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason Reason
		want   string
	}{
		// Platform-side.
		{ReasonQueuedExpired, "queued_expired"},
		{ReasonRuntimeOffline, "runtime_offline"},
		{ReasonRuntimeRecovery, "runtime_recovery"},
		{ReasonTimeout, "timeout"},
		{ReasonIterationLimit, "iteration_limit"},
		{ReasonAgentBlocked, "agent_blocked"},
		{ReasonAPIInvalidRequest, "api_invalid_request"},
		// Agent-side.
		{ReasonAgentProviderAuthOrAccess, "agent_error.provider_auth_or_access"},
		{ReasonAgentProviderQuotaLimit, "agent_error.provider_quota_limit"},
		{ReasonAgentProviderCapacityOrRateLimit, "agent_error.provider_capacity_or_rate_limit"},
		{ReasonAgentProviderServerError, "agent_error.provider_server_error"},
		{ReasonAgentProviderNetwork, "agent_error.provider_network"},
		{ReasonAgentProcessFailure, "agent_error.process_failure"},
		{ReasonAgentEmptyOrUnparseableOutput, "agent_error.empty_or_unparseable_output"},
		{ReasonAgentTimeout, "agent_error.agent_timeout"},
		{ReasonAgentContextOverflow, "agent_error.context_overflow"},
		{ReasonAgentMissingConfig, "agent_error.missing_config"},
		{ReasonAgentModelNotFoundOrUnavailable, "agent_error.model_not_found_or_unavailable"},
		{ReasonAgentRuntimeVersionUnsupported, "agent_error.runtime_version_unsupported"},
		{ReasonAgentRuntimeMissingExecutable, "agent_error.runtime_missing_executable"},
		{ReasonAgentUnknown, "agent_error.unknown"},
	}

	if got, want := len(cases), 21; got != want {
		t.Fatalf("constant count = %d, want %d (canonical taxonomy size)", got, want)
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := c.reason.String(); got != c.want {
				t.Errorf("Reason(%q).String() = %q, want %q", c.reason, got, c.want)
			}
		})
	}
}

// TestIsAgentError pins the platform-side vs agent-side split so a
// future Prometheus collector / retry policy can rely on the prefix
// rather than maintaining a parallel allow-list.
func TestIsAgentError(t *testing.T) {
	t.Parallel()

	platformSide := []Reason{
		ReasonQueuedExpired,
		ReasonRuntimeOffline,
		ReasonRuntimeRecovery,
		ReasonTimeout,
		ReasonIterationLimit,
		ReasonAgentBlocked,
		ReasonAPIInvalidRequest,
	}
	for _, r := range platformSide {
		if r.IsAgentError() {
			t.Errorf("%q.IsAgentError() = true, want false (platform-side)", r)
		}
	}

	agentSide := []Reason{
		ReasonAgentProviderAuthOrAccess,
		ReasonAgentProviderQuotaLimit,
		ReasonAgentProviderCapacityOrRateLimit,
		ReasonAgentProviderServerError,
		ReasonAgentProviderNetwork,
		ReasonAgentProcessFailure,
		ReasonAgentEmptyOrUnparseableOutput,
		ReasonAgentTimeout,
		ReasonAgentContextOverflow,
		ReasonAgentMissingConfig,
		ReasonAgentModelNotFoundOrUnavailable,
		ReasonAgentRuntimeVersionUnsupported,
		ReasonAgentRuntimeMissingExecutable,
		ReasonAgentUnknown,
	}
	for _, r := range agentSide {
		if !r.IsAgentError() {
			t.Errorf("%q.IsAgentError() = false, want true (agent-side)", r)
		}
		if !strings.HasPrefix(r.String(), "agent_error.") {
			t.Errorf("%q missing required agent_error. prefix", r)
		}
	}
}

// TestAllReasonsContents verifies that AllReasons() returns the
// complete canonical taxonomy with no duplicates and no surprise
// values. Prometheus pre-warming relies on this fixture being stable.
func TestAllReasonsContents(t *testing.T) {
	t.Parallel()

	got := AllReasons()
	if len(got) != 21 {
		t.Fatalf("AllReasons() returned %d entries, want 21", len(got))
	}

	seen := make(map[Reason]bool, len(got))
	var platformCount, agentCount int
	for _, r := range got {
		if seen[r] {
			t.Errorf("AllReasons() returned duplicate %q", r)
		}
		seen[r] = true
		if r.IsAgentError() {
			agentCount++
		} else {
			platformCount++
		}
	}

	if platformCount != 7 {
		t.Errorf("AllReasons(): platform-side count = %d, want 7", platformCount)
	}
	if agentCount != 14 {
		t.Errorf("AllReasons(): agent-side count = %d, want 14", agentCount)
	}

	// Sanity-check that every constant declared at package level
	// shows up in AllReasons. This catches a future drift where
	// someone adds a constant but forgets to register it in the
	// allReasons slice.
	required := []Reason{
		ReasonQueuedExpired, ReasonRuntimeOffline, ReasonRuntimeRecovery,
		ReasonTimeout, ReasonIterationLimit, ReasonAgentBlocked,
		ReasonAPIInvalidRequest,
		ReasonAgentProviderAuthOrAccess, ReasonAgentProviderQuotaLimit,
		ReasonAgentProviderCapacityOrRateLimit, ReasonAgentProviderServerError,
		ReasonAgentProviderNetwork, ReasonAgentProcessFailure,
		ReasonAgentEmptyOrUnparseableOutput, ReasonAgentTimeout,
		ReasonAgentContextOverflow, ReasonAgentMissingConfig,
		ReasonAgentModelNotFoundOrUnavailable,
		ReasonAgentRuntimeVersionUnsupported,
		ReasonAgentRuntimeMissingExecutable,
		ReasonAgentUnknown,
	}
	for _, r := range required {
		if !seen[r] {
			t.Errorf("AllReasons() missing canonical reason %q", r)
		}
	}
}

// TestAllReasonsIsDefensiveCopy guards the contract that mutating the
// returned slice cannot corrupt the package-level fixture. Without
// this, two callers (e.g. two Prometheus collectors at startup) could
// race on a shared slice.
func TestAllReasonsIsDefensiveCopy(t *testing.T) {
	t.Parallel()

	first := AllReasons()
	if len(first) == 0 {
		t.Fatal("AllReasons() returned empty slice")
	}
	original := first[0]
	first[0] = "tampered"

	second := AllReasons()
	if second[0] == "tampered" {
		t.Fatalf("AllReasons() leaked package state: second call returned tampered value %q", second[0])
	}
	if second[0] != original {
		t.Fatalf("AllReasons()[0] = %q, want %q", second[0], original)
	}
}
