package handler

import "testing"

// TestAutopilotRunReasonCode pins the mapping from a run's status/failure_reason
// to the stable, enumeration-safe DispatchReasonCode the "run now" UI localizes
// (MUL-4525). The raw English reason must never reach the wire; every skip/fail
// phrasing the dispatch layer produces must classify to a known code, and an
// unrecognized reason must fall back to internal_error rather than leak text.
func TestAutopilotRunReasonCode(t *testing.T) {
	ptr := func(s string) *string { return &s }
	cases := []struct {
		name    string
		status  string
		reason  *string
		wantNil bool
		want    DispatchReasonCode
	}{
		{"success has no code", "completed", nil, true, ""},
		{"issue_created has no code", "issue_created", nil, true, ""},
		{"running has no code", "running", nil, true, ""},
		{"creator lacks access", "skipped", ptr("autopilot creator lacks access to private assignee agent"), false, ReasonInvocationNotAllowed},
		{"clicker not allowed to trigger", "skipped", ptr("you are not allowed to trigger this autopilot's assignee agent"), false, ReasonInvocationNotAllowed},
		{"squad leader not allowed to invoke", "failed", ptr("not allowed to invoke private squad leader"), false, ReasonInvocationNotAllowed},
		{"runtime offline", "skipped", ptr("assignee agent runtime is offline at dispatch time"), false, ReasonRuntimeOffline},
		{"no assignee", "skipped", ptr("autopilot has no assignee"), false, ReasonTargetUnavailable},
		{"squad archived", "skipped", ptr("assignee squad is archived"), false, ReasonTargetUnavailable},
		{"agent gone", "skipped", ptr("assignee agent no longer exists"), false, ReasonTargetUnavailable},
		{"unknown reason falls back", "failed", ptr("dispatch create_issue: boom"), false, ReasonInternalError},
		{"failed with nil reason", "failed", nil, false, ReasonInternalError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := autopilotRunReasonCode(tc.status, tc.reason)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %q", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %q, got nil", tc.want)
			}
			if *got != string(tc.want) {
				t.Errorf("got %q, want %q", *got, tc.want)
			}
		})
	}
}

// TestDispatchBlockedFallbackMessageIsNonEnumerating asserts the legacy `error`
// string for every reason code stays generic — it must be safe to show to a
// caller who is not allowed to know whether the target exists, so it must not
// name a private agent, its owner, or reveal existence.
func TestDispatchBlockedFallbackMessageIsNonEnumerating(t *testing.T) {
	codes := []DispatchReasonCode{
		ReasonInvocationNotAllowed, ReasonTargetUnavailable, ReasonRuntimeOffline,
		ReasonAttributionBlocked, ReasonAlreadyActive, ReasonInternalError,
		DispatchReasonCode("some_future_code"),
	}
	for _, c := range codes {
		msg := dispatchBlockedFallbackMessage(c)
		if msg == "" {
			t.Errorf("reason %q: empty fallback message", c)
		}
	}
	// invocation_not_allowed must be deliberately vague: it cannot distinguish
	// "target is private" from "target does not exist".
	if got := dispatchBlockedFallbackMessage(ReasonInvocationNotAllowed); got != "you are not allowed to trigger this target" {
		t.Errorf("invocation_not_allowed fallback = %q, changed to something more revealing?", got)
	}
}
