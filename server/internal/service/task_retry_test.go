package service

import "testing"

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		errMsg  string
		want    FailureClass
		comment string
	}{
		// Structured reason wins.
		{"structured runtime_offline", "runtime_offline", "", FailureTransient, "sweeper writes this"},
		{"structured timed_out", "timed_out", "", FailureTransient, ""},
		{"structured rate_limited", "rate_limited", "irrelevant", FailureRateLimited, "daemon classifies 429 → typed reason"},
		{"sweeper text reason runtime", "runtime went offline", "", FailureTransient, ""},
		{"sweeper text reason timeout", "task timed out", "", FailureTransient, ""},

		// agent_error catch-all → fall through to error string.
		{"agent_error 429 in body", "agent_error", "API Error: Request rejected (429) · rate limit", FailureRateLimited, "the schieber-blocker case"},
		{"agent_error rate limit phrase", "agent_error", "exceed your organization's rate limit of 30000 input tokens", FailureRateLimited, ""},
		{"agent_error quota phrase", "agent_error", "quota exceeded for project foo", FailureRateLimited, ""},
		{"agent_error too many requests", "agent_error", "Too Many Requests", FailureRateLimited, ""},
		{"agent_error transient timeout", "agent_error", "context deadline exceeded", FailureTransient, ""},
		{"agent_error broken pipe", "agent_error", "write tcp: broken pipe", FailureTransient, ""},
		{"agent_error connection reset", "agent_error", "read: connection reset by peer", FailureTransient, ""},
		{"agent_error i/o timeout", "agent_error", "dial: i/o timeout", FailureTransient, ""},
		{"agent_error real bug", "agent_error", "panic: nil pointer dereference", FailurePermanent, "real code bug = no retry"},
		{"agent_error bad input", "agent_error", "invalid issue id", FailurePermanent, ""},

		// No reason set, classifier still works on errMsg alone.
		{"empty reason 429 numeric", "", "API status 429", FailureRateLimited, ""},
		{"empty reason empty err", "", "", FailurePermanent, "nothing to classify → permanent"},

		// Permanent-class structured reasons must not retry.
		{"structured permanent", "permanent", "anything", FailurePermanent, ""},

		// Case-insensitivity on errMsg pattern matching.
		{"uppercase rate limit", "agent_error", "RATE LIMIT EXCEEDED", FailureRateLimited, ""},
		{"mixed case ratelimit", "agent_error", "RateLimit error: try again later", FailureRateLimited, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFailure(tt.reason, tt.errMsg)
			if got != tt.want {
				t.Errorf("ClassifyFailure(%q, %q) = %s, want %s",
					tt.reason, tt.errMsg, got, tt.want)
			}
		})
	}
}

func TestFailureClass_String(t *testing.T) {
	tests := []struct {
		c    FailureClass
		want string
	}{
		{FailurePermanent, "permanent"},
		{FailureTransient, "transient"},
		{FailureRateLimited, "rate_limited"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("FailureClass(%d).String() = %q, want %q", tt.c, got, tt.want)
		}
	}
}
