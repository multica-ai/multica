package taskfailure

import (
	"strings"
	"testing"
)

func TestTraePromptStreamDisconnect(t *testing.T) {
	const prefix = "traecli session/prompt failed: session/prompt: Internal error (code=-32603, data="
	const emptyStream = "stream disconnected before completion: llm response empty stream"
	const cut = `{"trae_error_info":{"response_stream_disconnected":{"http_status_code":null}}}`
	const cutAfterOK = `{"trae_error_info":{"response_stream_disconnected":{"http_status_code":200}},"additional_details":"` + emptyStream + `"}`
	const forbidden = `{"trae_error_info":{"response_stream_disconnected":{"http_status_code":403}}}`
	const unavailable = `{"trae_error_info":{"response_stream_disconnected":{"http_status_code":503}}}`
	const limited = `{"trae_error_info":{"response_stream_disconnected":{}},"additional_details":"rate limit exceeded"}`
	prompt := func(attempts ...string) string {
		return prefix + `{"message":"` + emptyStream + `","trae_error_info":"other","trae_stream_errors":[` + strings.Join(attempts, ",") + `]})`
	}
	confirmed := prompt(cut, cutAfterOK)
	for _, tc := range []struct {
		name string
		raw  string
		want Reason
	}{
		{"confirmed disconnects", confirmed, ReasonAgentProviderNetwork},
		{"typed primary cause", strings.Replace(confirmed, `"other"`, `{"response_stream_disconnected":{}}`, 1), ReasonAgentProviderNetwork},
		{"different primary cause", strings.Replace(confirmed, `"other"`, `"invalid_request"`, 1), ReasonAgentProviderServerError},
		{"primary HTTP error", strings.Replace(confirmed, `"other"`, `{"response_stream_disconnected":{"http_status_code":503}}`, 1), ReasonAgentProviderServerError},
		{"earlier access denial", prompt(forbidden, cut), ReasonAgentProviderAuthOrAccess},
		{"later HTTP failure", prompt(cut, unavailable), ReasonAgentProviderServerError},
		{"provider error alongside confirmed disconnects", strings.Replace(confirmed, `"trae_stream_errors"`, `"detail":"HTTP 503","trae_stream_errors"`, 1), ReasonAgentProviderServerError},
		{"rate limit in reconnect details", prompt(limited, cut), ReasonAgentProviderCapacityOrRateLimit},
		{"different reconnect cause", prompt(cut, `{"trae_error_info":"other"}`), ReasonAgentProviderServerError},
		{"different reconnect details", prompt(`{"trae_error_info":{"response_stream_disconnected":{}},"additional_details":"invalid request"}`), ReasonAgentProviderServerError},
		{"empty reconnect history", prompt(), ReasonAgentProviderServerError},
		{"missing cause", prompt(`{"additional_details":"` + emptyStream + `"}`), ReasonAgentProviderServerError},
		{"different prompt error", strings.Replace(confirmed, emptyStream, "invalid request", 1), ReasonAgentProviderServerError},
		{"wrong RPC code", strings.Replace(confirmed, "-32603", "-32602", 1), ReasonAgentProviderServerError},
		{"different method", strings.ReplaceAll(confirmed, "session/prompt", "session/load"), ReasonAgentProviderServerError},
		{"quoted tool output", "tool output: " + confirmed, ReasonAgentProviderServerError},
		{"malformed data", strings.TrimSuffix(confirmed, "})") + ")", ReasonAgentProviderServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.raw); got != tc.want {
				t.Errorf("Classify() = %q, want %q", got, tc.want)
			}
			if got := NormalizeDaemonReason(ReasonAgentProviderServerError.String(), tc.raw); got != tc.want {
				t.Errorf("NormalizeDaemonReason() = %q, want %q", got, tc.want)
			}
		})
	}

	for _, legacy := range []string{"", "agent_error", ReasonAgentUnknown.String()} {
		if got := NormalizeDaemonReason(legacy, confirmed); got != ReasonAgentProviderNetwork {
			t.Errorf("NormalizeDaemonReason(%q) = %q, want %q", legacy, got, ReasonAgentProviderNetwork)
		}
	}
	for _, specific := range []Reason{ReasonTimeout, ReasonAgentProviderAuthOrAccess, ReasonAgentProviderCapacityOrRateLimit} {
		if got := NormalizeDaemonReason(specific.String(), confirmed); got != specific {
			t.Errorf("NormalizeDaemonReason(%q) = %q, want %q", specific, got, specific)
		}
	}
}
