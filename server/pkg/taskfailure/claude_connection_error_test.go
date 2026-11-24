package taskfailure

import "testing"

// claudeConnectionDroppedErr is verbatim from failed Claude Code runs (GH #10781).
const claudeConnectionDroppedErr = "API Error: Connection dropped (ECONNRESET). A VPN, firewall, or security software can cause this. Check those or switch networks, then try again."

func TestClassifyClaudeConnectionError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want Reason
	}{
		{"dropped", claudeConnectionDroppedErr, ReasonAgentProviderNetwork},
		{
			"dropped behind a gateway",
			"API Error: Connection dropped (ECONNRESET). Check the gateway set in ANTHROPIC_BASE_URL, then try again.",
			ReasonAgentProviderNetwork,
		},
		{"no route", "API Error: No internet route — check your connection or VPN (ENETUNREACH)", ReasonAgentProviderNetwork},
		{
			"exit status does not make it a process failure",
			"claude exited with error: exit status 1\n" + claudeConnectionDroppedErr,
			ReasonAgentProviderNetwork,
		},
		{"provider evidence wins", "429 rate limit\n" + claudeConnectionDroppedErr, ReasonAgentProviderCapacityOrRateLimit},
		{"local tool is not provider evidence", "mcp tool returned: Connection dropped (ECONNRESET)", ReasonAgentUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.in); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An installed daemon predating the witnesses reports a non-retryable reason;
// the server upgrades it without waiting on the daemon fleet.
func TestNormalizeDaemonReasonUpgradesClaudeConnectionError(t *testing.T) {
	t.Parallel()

	for _, legacy := range []string{
		string(ReasonAgentProcessFailure),
		string(ReasonAgentUnknown),
		"agent_error",
	} {
		if got := NormalizeDaemonReason(legacy, claudeConnectionDroppedErr); got != ReasonAgentProviderNetwork {
			t.Errorf("NormalizeDaemonReason(%q) = %q, want %q", legacy, got, ReasonAgentProviderNetwork)
		}
	}

	// A more specific reason the daemon chose is left alone.
	if got := NormalizeDaemonReason(string(ReasonAgentProviderAuthOrAccess), claudeConnectionDroppedErr); got != ReasonAgentProviderAuthOrAccess {
		t.Errorf("auth reason was overwritten: got %q", got)
	}
	if got := NormalizeDaemonReason(string(ReasonAgentProcessFailure), "claude exited with error: exit status 1"); got != ReasonAgentProcessFailure {
		t.Errorf("plain process failure was upgraded: got %q", got)
	}
}
