package taskfailure

import (
	"strings"
	"testing"
)

func TestClassifyCursorConnectTimeout(t *testing.T) {
	t.Parallel()
	const stderr = "Error: [unavailable] connect ETIMEDOUT 192.0.2.1:443"
	const wrapped = "cursor-agent exited with error: exit status 1 (result_seen=false, exit_code=1, scanner_error=false, event_count=0, invalid_event_count=0, last_event_type=none); actions completed before finalization may already have taken effect; cursor stderr: " + stderr
	for _, input := range []string{stderr, wrapped, strings.ToUpper(wrapped)} {
		if got := Classify(input); got != ReasonAgentProviderNetwork {
			t.Errorf("Classify(%q) = %s, want %s", input, got, ReasonAgentProviderNetwork)
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
	} {
		if got := Classify(input); got == ReasonAgentProviderNetwork {
			t.Errorf("Classify(%q) must not classify unrelated tool failures as provider network errors", input)
		}
	}
}
