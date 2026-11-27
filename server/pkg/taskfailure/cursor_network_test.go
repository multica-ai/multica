package taskfailure

import (
	"fmt"
	"strings"
	"testing"
)

func TestClassifyCursorConnectTimeout(t *testing.T) {
	t.Parallel()
	const stderr = "Error: [unavailable] connect ETIMEDOUT 192.0.2.1:443"
	const wrapped = "cursor-agent exited with error: exit status 1 (result_seen=false, exit_code=1, scanner_error=false, event_count=0, invalid_event_count=0, last_event_type=none); actions completed before finalization may already have taken effect; cursor stderr: " + stderr
	const writeFailure = "cursor-agent prompt write failed: write |1: broken pipe; cursor stderr: " + stderr
	for _, input := range []string{stderr, wrapped, strings.ToUpper(wrapped), writeFailure} {
		if got := Classify(input); got != ReasonAgentProviderNetwork {
			t.Errorf("Classify(%q) = %s, want %s", input, got, ReasonAgentProviderNetwork)
		}
		if got := NormalizeDaemonReason(ReasonAgentProcessFailure.String(), input); got != ReasonAgentProviderNetwork {
			t.Errorf("NormalizeDaemonReason(process_failure, %q) = %s, want %s", input, got, ReasonAgentProviderNetwork)
		}
	}
}

func TestCursorTransportDiagnosticCountsAreNotHTTPStatus(t *testing.T) {
	t.Parallel()
	for _, count := range []int{401, 402, 403, 429, 500, 529, 599} {
		raw := fmt.Sprintf("cursor-agent exited with error: exit status 1 (result_seen=false, exit_code=1, scanner_error=false, event_count=%d, invalid_event_count=0, last_event_type=system); actions completed before finalization may already have taken effect; cursor stderr: RetriableError: Connection stalled", count)
		if got := Classify(raw); got != ReasonAgentProviderNetwork {
			t.Errorf("event_count=%d classified as %s, want provider_network", count, got)
		}
		header, _, _ := strings.Cut(raw, "; cursor stderr: ")
		legacy := Classify(header)
		if got := NormalizeDaemonReason(legacy.String(), raw); got != ReasonAgentProviderNetwork {
			t.Errorf("event_count=%d: NormalizeDaemonReason(%s) = %s, want provider_network", count, legacy, got)
		}
	}
}

func TestClassifyCursorConnectTimeoutScope(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"local tool failed: connect ETIMEDOUT 192.0.2.1:443",
		"MCP server error: Error: [unavailable] connect ETIMEDOUT 192.0.2.1:443",
		"cursor-agent exited with error: exit status 1; cursor stderr: local tool failed: connect ETIMEDOUT 192.0.2.1:443",
		"cursor-agent exited with error: exit status 1; cursor stderr: Error: [unavailable] connect ETIMEDOUT_OTHER 192.0.2.1:443",
		"some tool exited with error: exit status 1; cursor stderr: Error: [unavailable] connect ETIMEDOUT 192.0.2.1:443",
		"cursor-agent prompt write failed: write |1: broken pipe",
		"cursor-agent prompt write failed: write |1: broken pipe; cursor stderr: local tool failed: connect ETIMEDOUT 192.0.2.1:443",
	} {
		if got := Classify(input); got == ReasonAgentProviderNetwork {
			t.Errorf("Classify(%q) must not classify unrelated tool failures as provider network errors", input)
		}
	}
}

func TestClassifyCursorConnectionStalled(t *testing.T) {
	t.Parallel()
	for _, stderr := range []string{
		"RetriableError: Connection stalled",
		"RetriableError: Connection stalled repeatedly",
		"RetriableError: Connection stalled\n    at transport (/cursor/index.js:1:1)",
		"RetriableError: Connection stalled\n    at transport (/cursor/index.js:500:403)",
	} {
		for _, input := range []string{
			stderr,
			"cursor-agent exited with error: exit status 1 (result_seen=false, exit_code=1, scanner_error=false, event_count=463, invalid_event_count=0, last_event_type=system); actions completed before finalization may already have taken effect; cursor stderr: " + stderr,
			"cursor-agent prompt write failed: write |1: broken pipe; cursor stderr: " + stderr,
			strings.ToUpper(stderr),
			strings.ToUpper("cursor-agent exited with error: exit status 1; cursor stderr: " + stderr),
		} {
			if got := Classify(input); got != ReasonAgentProviderNetwork {
				t.Errorf("Classify(%q) = %s, want %s", input, got, ReasonAgentProviderNetwork)
			}
			for _, legacy := range []Reason{ReasonAgentProcessFailure, ReasonAgentUnknown, "agent_error"} {
				if got := NormalizeDaemonReason(legacy.String(), input); got != ReasonAgentProviderNetwork {
					t.Errorf("NormalizeDaemonReason(%s, %q) = %s, want %s", legacy, input, got, ReasonAgentProviderNetwork)
				}
			}
		}
	}
}

func TestCursorConnectionStalledScope(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"Connection stalled",
		"RetriableError: Something else",
		"RetriableError: Connection stalled_OTHER",
		"RetriableError: Connection stalled in a local tool",
		"MCP server error: RetriableError: Connection stalled",
		"some tool exited with error: exit status 1; cursor stderr: RetriableError: Connection stalled",
		"cursor-agent exited with error: exit status 1; cursor stderr: local tool failed: RetriableError: Connection stalled",
		"cursor-agent prompt write failed: broken pipe; cursor stderr: MCP server error: RetriableError: Connection stalled",
		"cursor-agent exited with error: exit status 1; RetriableError: Connection stalled",
		"RetriableError: Connection stalled\nAPI Error: 403 Forbidden",
	} {
		if got := Classify(input); got == ReasonAgentProviderNetwork {
			t.Errorf("Classify(%q) must not classify unrelated failures as provider network errors", input)
		}
		if got := NormalizeDaemonReason(ReasonAgentProcessFailure.String(), input); got != ReasonAgentProcessFailure {
			t.Errorf("NormalizeDaemonReason(process_failure, %q) = %s, want it preserved", input, got)
		}
	}
	stack := "RetriableError: Connection stalled\n    at transport (/cursor/index.js:500:403)"
	if got := NormalizeDaemonReason(ReasonAgentProviderAuthOrAccess.String(), stack); got != ReasonAgentProviderNetwork {
		t.Errorf("NormalizeDaemonReason(auth, stalled with diagnostic stack location) = %s, want provider_network", got)
	}
	for _, reason := range []Reason{ReasonAgentProviderAuthOrAccess, ReasonAgentProviderQuotaLimit, ReasonAgentContextOverflow, ReasonAgentTimeout} {
		if got := NormalizeDaemonReason(reason.String(), "RetriableError: Connection stalled"); got != reason {
			t.Errorf("NormalizeDaemonReason(%s, stalled) = %s, want it preserved", reason, got)
		}
		wrapped := "cursor-agent exited with error: exit status 1; cursor stderr: RetriableError: Connection stalled"
		if got := NormalizeDaemonReason(reason.String(), wrapped); got != reason {
			t.Errorf("NormalizeDaemonReason(%s, wrapped stalled without diagnostic count) = %s, want it preserved", reason, got)
		}
	}
}
