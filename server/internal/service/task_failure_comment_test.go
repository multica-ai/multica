package service

import "testing"

func TestSuppressFailureIssueComment(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "provider invalid request", reason: "api_invalid_request", want: true},
		{name: "provider auth failure", reason: "agent_error.provider_auth_or_access", want: true},
		{name: "provider quota failure", reason: "agent_error.provider_quota_limit", want: true},
		{name: "agent process failure", reason: "agent_error.process_failure", want: false},
		{name: "runtime offline", reason: "runtime_offline", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := suppressFailureIssueComment(tt.reason); got != tt.want {
				t.Fatalf("suppressFailureIssueComment(%q) = %v, want %v", tt.reason, got, tt.want)
			}
		})
	}
}
