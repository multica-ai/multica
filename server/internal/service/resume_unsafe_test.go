package service

import "testing"

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
