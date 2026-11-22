package taskfailure

import "testing"

func TestInterruptedRunTimeoutRecoveryClassification(t *testing.T) {
	errors := []string{
		"workspace routing discovery timed out",
		"codex thread/resume failed: codex app-server handshake timeout: thread/resume did not respond after 1m0s",
		"codex thread/start failed: codex app-server handshake timeout: thread/start did not respond after 1m0s",
	}
	for _, raw := range errors {
		t.Run(raw, func(t *testing.T) {
			if got := Classify(raw); got != ReasonTimeout {
				t.Errorf("Classify = %s, want %s", got, ReasonTimeout)
			}
			for _, legacy := range []string{"agent_error", ReasonAgentUnknown.String()} {
				if got := NormalizeDaemonReason(legacy, raw); got != ReasonTimeout {
					t.Errorf("NormalizeDaemonReason(%s) = %s, want %s", legacy, got, ReasonTimeout)
				}
			}
		})
	}
}

func TestInterruptedRunTimeoutDoesNotRetryUnrelatedFailures(t *testing.T) {
	for _, raw := range []string{
		"workspace routing discovery failed",
		"workspace routing discovery timed out: unauthorized",
		"codex turn/start failed: codex app-server handshake timeout: turn/start did not respond after 1m0s",
		"codex thread/start failed: context deadline exceeded",
		"codex app-server handshake timeout",
		"codex thread/start failed: codex app-server handshake timeout: thread/start did not respond after ",
		"codex thread/start failed: codex app-server handshake timeout: thread/start did not respond after invalid api key",
		"agent timed out after 2h",
	} {
		if IsInterruptedRunTimeout(raw) {
			t.Errorf("unrelated failure accepted: %q", raw)
		}
	}
	const raw = "workspace routing discovery timed out"
	if got := NormalizeDaemonReason(ReasonAgentProviderAuthOrAccess.String(), raw); got != ReasonAgentProviderAuthOrAccess {
		t.Errorf("explicit failure reason overwritten: %s", got)
	}
}
