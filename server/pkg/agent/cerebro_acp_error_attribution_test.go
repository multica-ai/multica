package agent

// CEREBRO-PATCH(acp-error-attribution): FIR-3651 feedback loop for the
// mis-attributed-failure bug. These tests replay the exact stderr
// sequence of production run 17fe5ffb-4e1d-4d7e-988a-474e89b5ea63
// (Kimi3 on Hermes, 2026-07-22): a harmless terminal-tool failure early
// in the run, then the real terminal failure — an upstream HTTP 429 —
// almost a minute later. The task was surfaced to the user with the
// harmless line as its cause.

import (
	"strings"
	"testing"
)

// toolResultLine is the stderr hermes emits when a terminal tool the
// agent ran exits non-zero. It is ordinary run output: the agent saw
// it, corrected itself 11 seconds later, and kept working.
const toolResultLine = `terminal result - **output:** Error: write file: open argus-v2.1.0.html/argus-architecture-v2.1.0.html: no such file or directory", "exit_code": 1, "error": null}`

// terminalProviderLine is the real cause of death: the adapter gave up
// against the upstream LLM after exhausting its retries.
const terminalProviderLine = `❌ API call failed after 3 retries: HTTP 429: Provider returned error`

// TestSnifferReportsTerminalErrorNotFirstSeen pins the user-visible
// symptom: the failure message must name the error that actually ended
// the run, not the oldest line the sniffer happened to capture.
func TestSnifferReportsTerminalErrorNotFirstSeen(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	for _, line := range []string{toolResultLine, terminalProviderLine} {
		if _, err := s.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	msg := s.terminalMessage()
	if strings.Contains(msg, "write file") {
		t.Errorf("failure attributed to an already-recovered tool error: %q", msg)
	}
	if !strings.Contains(msg, "429") {
		t.Errorf("expected the terminal 429 as the reported cause, got %q", msg)
	}
}

// TestSnifferIgnoresTerminalToolOutput pins the upstream cause: a
// failing command inside the run is not a provider error and must not
// be captured as one. acpErrorDetailRe currently matches any line
// containing "Error:", including this one.
func TestSnifferIgnoresTerminalToolOutput(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	if _, err := s.Write([]byte(toolResultLine + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if msg := s.message(); msg != "" {
		t.Errorf("a failing terminal command was captured as a provider error: %q", msg)
	}
}
