package taskfailure

import "testing"

func TestClassifyProviderContentFilter(t *testing.T) {
	t.Parallel()
	want := ReasonAgentProviderContentFilter
	for _, message := range []string{
		"stream disconnected before completion: Incomplete response returned, reason: content_filter",
		"codex exited with error: exit status 1; Incomplete response returned, reason: CONTENT_FILTER",
		`API Error: 400 {"error":{"code":"content_filter","message":"The provider blocked the request"}}`,
		`{"status":"incomplete","incomplete_details":{"reason":"content_filter"}}`,
		`{"finish_reason": "content_filter"}`,
		"reason = 'content_filter'",
		`API Error: 403 {"error":{"code":"content_filter"}}`,
		`{"code":"content_filter","message":"token limit diagnostics"}`,
	} {
		t.Run(message, func(t *testing.T) {
			if got := Classify(message); got != want {
				t.Errorf("Classify(%q) = %q, want %q", message, got, want)
			}
		})
	}
	for _, message := range []string{
		"stream disconnected before completion",
		"stream disconnected before completion: Incomplete response returned, reason: max_output_tokens",
		"exit status 1: test failed in content_filter.go",
		"read /tmp/content_filter/config.json: no such file",
		"failed to read content filter settings",
		"content_filter",
		"reason: content_filter_disabled",
		"reason: content_filter.go",
		"error_code: content_filter",
	} {
		t.Run("negative/"+message, func(t *testing.T) {
			if got := Classify(message); got == want {
				t.Errorf("Classify(%q) should not classify an unrelated token as content filtering", message)
			}
		})
	}
}

func TestNormalizeLegacyContentFilterReason(t *testing.T) {
	t.Parallel()
	const message = "stream disconnected before completion: Incomplete response returned, reason: content_filter"
	want := ReasonAgentProviderContentFilter
	for _, reason := range []Reason{ReasonAgentProviderNetwork, ReasonAgentProcessFailure, ReasonAgentUnknown, Reason("agent_error")} {
		if got := NormalizeDaemonReason(reason.String(), message); got != want {
			t.Errorf("NormalizeDaemonReason(%q) = %q, want %q", reason, got, want)
		}
	}
	for _, reason := range []Reason{ReasonTimeout, ReasonRuntimeOffline, ReasonAgentTimeout, ReasonAgentProviderAuthOrAccess,
		ReasonAgentProviderQuotaLimit, ReasonAgentProviderCapacityOrRateLimit, ReasonAgentProviderServerError, ReasonAgentContextOverflow} {
		if got := NormalizeDaemonReason(reason.String(), message); got != reason {
			t.Errorf("NormalizeDaemonReason(%q) overwrote a more specific reason with %q", reason, got)
		}
	}
	if got := NormalizeDaemonReason(ReasonAgentProviderNetwork.String(), "stream disconnected before completion"); got != ReasonAgentProviderNetwork {
		t.Errorf("real network failure changed to %q", got)
	}
}
