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

// noteNotFoundToolLine is the stderr hermes emitted at 20:09 on the same
// production run (issue 6af63422): the agent's read-note tool got a 404
// because the note id did not exist. Like the write-file failure, it is
// ordinary run output the agent recovers from — its "returned 404" wording
// carries no "HTTP <code>" token and no ⚠️/❌ banner, so it must not be
// captured as a provider error and must not turn the run red.
const noteNotFoundToolLine = `terminal result - **output:** Error: read note: GET /api/notes/019f8341-e3f6-7db7-88a2-e52c41f8f25a returned 404: {"error":"note not found"}", "exit_code": 1, "error": null}`

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

// TestSnifferIgnoresNoteNotFound404 pins the exact case Jesper reported:
// a read-note tool that got a 404 ("note not found") must not become the
// cause of a failed run. It is a recoverable tool error, not a provider
// failure, and neither message() nor promoteACPResultOnProviderError may
// flip a completed run to failed on the strength of it.
func TestSnifferIgnoresNoteNotFound404(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	if _, err := s.Write([]byte(noteNotFoundToolLine + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if msg := s.message(); msg != "" {
		t.Errorf("a 404 note-not-found tool error was captured as a provider error: %q", msg)
	}

	// The run completed with real output; the 404 line must not promote it.
	status, errMsg := promoteACPResultOnProviderError("completed", "", "done writing the diagram", s)
	if status != "completed" {
		t.Errorf("run flipped to %q on a recoverable 404 (error=%q); expected it to stay completed", status, errMsg)
	}
}

// TestSnifferKeepsHeaderlessProviderError guards the header gate from
// over-reaching: an adapter that spells out a provider failure without first
// printing a ⚠️/❌ banner must still be captured, or the run reports empty
// output instead of the real failure.
func TestSnifferKeepsHeaderlessProviderError(t *testing.T) {
	t.Parallel()

	s := newACPProviderErrorSniffer("hermes")
	if _, err := s.Write([]byte("Error: HTTP 503: upstream is unavailable\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if msg := s.message(); !strings.Contains(msg, "503") {
		t.Errorf("headerless provider error was dropped, got %q", msg)
	}
}
