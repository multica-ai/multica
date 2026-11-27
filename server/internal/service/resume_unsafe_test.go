package service

import "testing"

func TestResumeUnsafeFailureIgnoresCodebuddyProtocolSummary(t *testing.T) {
	const summary = "\n[codebuddy protocol: exit_code=0 stdout_bytes=1400 stderr_bytes=0 event_count=1 invalid_event_count=0 assistant_event_count=0 tool_use_count=0 saw_result=true result_is_error=true result_bytes=0 last_assistant_bytes=0 scanner_error=false]"
	for _, tc := range []struct {
		name, reason, raw string
		want              bool
	}{
		{"model", "agent_error.model_not_found_or_unavailable", `HTTP 404 {"type":"invalid_request_error","code":"model_not_found"}`, false},
		{"auth", "agent_error.provider_auth_or_access", `HTTP 401 {"type":"invalid_request_error","code":"invalid_api_key"}`, false},
		{"actual poisoned request", "agent_error.unknown", `HTTP 400 {"type":"invalid_request_error","message":"Could not process image"}`, true},
		{"already retired reason", "api_invalid_request", "no details", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResumeUnsafeFailure(tc.reason, tc.raw+summary); got != tc.want {
				t.Errorf("ResumeUnsafeFailure = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResumeUnsafeFailureEmptyHistoryMessage covers the manual-retry half of
// GH #6066. The claim handler resolves a rerun's session from the exact source
// task via ResumeUnsafeFailure rather than GetLastTaskSession, so a gap here
// means clicking Rerun resumes the very transcript the provider just refused —
// which is what the reporter experienced.
//
// The failure_reason cases and the raw-text cases are both exercised because
// they cover different hosts: a current daemon writes api_invalid_request, an
// older one (self-host installs upgrade on their own cadence) writes
// agent_error.unknown and only the text guard saves it.
func TestResumeUnsafeFailureEmptyHistoryMessage(t *testing.T) {
	t.Parallel()

	const gh6066 = "Invalid request: the message at position 37 with role 'assistant' must not be empty"
	const gh5760 = "kimi provider error: provider.api_error: 400 the message at position 43 with role 'assistant' must not be empty"

	cases := []struct {
		name          string
		failureReason string
		errorText     string
		want          bool
	}{
		{
			name:          "current daemon classifies the reason",
			failureReason: "api_invalid_request",
			errorText:     gh6066,
			want:          true,
		},
		{
			// The row an un-upgraded daemon writes: the text guard is the
			// only thing standing between Rerun and the poisoned session.
			name:          "older daemon leaves the reason unknown",
			failureReason: "agent_error.unknown",
			errorText:     gh6066,
			want:          true,
		},
		{
			name:          "kimi wording, unknown reason",
			failureReason: "agent_error.unknown",
			errorText:     gh5760,
			want:          true,
		},
		{
			// A provider that cannot resolve its own authentication method is
			// deterministic on resume: the missing api_key / auth_token / header is
			// baked into the session's provider state, so a rerun must start fresh
			// instead of replaying the same auth error. Every daemon version
			// classifies this text as agent_error.unknown (resume-safe), so the text
			// guard here is the only thing that un-wedges an issue stuck before (or
			// after) this fix deployed.
			name:          "auth-resolution text flips resume even when the reason is unknown",
			failureReason: "agent_error.unknown",
			errorText:     "hermes provider error: \"Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted\"",
			want:          true,
		},
		{
			// The guard is the load-bearing layer, deliberately reason-independent:
			// it must fire no matter what failure_reason a row happens to carry, so
			// it is not coupled to a classification that could drift.
			name:          "text guard is reason-independent",
			failureReason: "agent_error.missing_config",
			errorText:     "hermes provider error: \"Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted\"",
			want:          true,
		},
		{
			// Transient failures must stay resumable: the whole point of
			// reusing the session on retry is to keep the conversation.
			name:          "provider network drop stays resumable",
			failureReason: "agent_error.provider_network",
			errorText:     "Connection closed mid-response",
			want:          false,
		},
		{
			name:          "tool emptiness error stays resumable",
			failureReason: "agent_error.unknown",
			errorText:     "validation error: field must not be empty",
			want:          false,
		},
		{
			name:          "no error text and a benign reason",
			failureReason: "agent_error.unknown",
			errorText:     "",
			want:          false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResumeUnsafeFailure(tc.failureReason, tc.errorText); got != tc.want {
				t.Fatalf("ResumeUnsafeFailure(%q, %q) = %v, want %v", tc.failureReason, tc.errorText, got, tc.want)
			}
		})
	}
}
