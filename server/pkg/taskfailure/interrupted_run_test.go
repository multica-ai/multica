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
			want := ReasonProcessCleanupUnconfirmed
			if raw == "workspace routing discovery timed out" {
				want = ReasonTimeout
			}
			if got := Classify(raw); got != want {
				t.Errorf("Classify = %s, want %s", got, want)
			}
			for _, legacy := range []string{"agent_error", ReasonAgentUnknown.String()} {
				if got := NormalizeDaemonReason(legacy, raw); got != want {
					t.Errorf("NormalizeDaemonReason(%s) = %s, want %s", legacy, got, want)
				}
			}
			if got := NormalizeDaemonReason(ReasonTimeout.String(), raw); got != ReasonTimeout {
				t.Errorf("explicit confirmed timeout reason overwritten: %s", got)
			}
		})
	}
}

func TestInterruptedRunUnconfirmedCleanupSuffix(t *testing.T) {
	for _, method := range []string{"thread/start", "thread/resume"} {
		raw := "codex " + method + " failed: codex app-server handshake timeout: " + method + " did not respond after 1m0s; retry suppressed: process cleanup/reap not confirmed"
		if got := Classify(raw); got != ReasonProcessCleanupUnconfirmed {
			t.Errorf("Classify(%q) = %s", raw, got)
		}
		if got := NormalizeDaemonReason("agent_error.unknown", raw); got != ReasonProcessCleanupUnconfirmed {
			t.Errorf("NormalizeDaemonReason(%q) = %s", raw, got)
		}
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
