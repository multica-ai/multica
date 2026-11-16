package taskfailure

import (
	"fmt"
	"testing"
)

func TestCodebuddyProtocolSummaryDoesNotClassifyCounts(t *testing.T) {
	for _, count := range []int{401, 402, 403, 429, 500, 529} {
		summary := fmt.Sprintf("\n[codebuddy protocol: exit_code=0 stdout_bytes=%d stderr_bytes=%d event_count=%d invalid_event_count=0 assistant_event_count=0 tool_use_count=0 saw_result=true result_is_error=true result_bytes=0 last_assistant_bytes=0 scanner_error=false]", count, count, count)
		for _, tc := range []struct {
			raw  string
			want Reason
		}{
			{"codebuddy returned an error result without details", ReasonAgentUnknown},
			{"unknown model test-model", ReasonAgentModelNotFoundOrUnavailable},
			{"HTTP 401: Unauthorized", ReasonAgentProviderAuthOrAccess},
			{"codebuddy exited with error: exit status 7", ReasonAgentProcessFailure},
		} {
			if got := Classify(tc.raw + summary); got != tc.want {
				t.Errorf("count=%d Classify(%q) = %s, want %s", count, tc.raw, got, tc.want)
			}
		}
		// A byte count is not evidence of the special HTTP-403 quota shape.
		if got := NormalizeDaemonReason(ReasonAgentUnknown.String(), "codebuddy usage limit"+summary); got != ReasonAgentUnknown {
			t.Errorf("count=%d normalized from summary alone: %s", count, got)
		}
		if got := NormalizeDaemonReason(ReasonAgentUnknown.String(), "HTTP 403: usage limit"+summary); got != ReasonAgentProviderQuotaLimit {
			t.Errorf("count=%d lost real quota evidence: %s", count, got)
		}
	}
}
