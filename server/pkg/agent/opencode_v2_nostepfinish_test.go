package agent

import (
	"log/slog"
	"strings"
	"testing"
)

// Regression tests for the OpenCode v2 stream shape where the CLI ends the
// run WITHOUT emitting step_finish (anomalyco/opencode#53207, observed with
// opencode 2.0.28 on the OpenCode Go provider path): the step opens, the
// answer text streams in full, then EOF. The process itself exits 0 — the
// caller fails the run if the exit was non-zero — so discarding the answer
// over the missing closing event is a false failure.
func TestOpencodeProcessEventsV2StreamWithoutStepFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default(), BuiltinRuntime: true, CLIVersion: "opencode v2.0.28"}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_v2","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_v2","part":{"type":"text","text":"The answer is 42."}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q (err %q), want %q", result.status, result.errMsg, "completed")
	}
	if result.output != "The answer is 42." {
		t.Errorf("output: got %q, want the streamed answer preserved", result.output)
	}
	if result.noTerminalSignal {
		t.Errorf("noTerminalSignal: got true, want false for a rescued clean-exit stream")
	}
}

// The rescue must stay narrow: a step that opens and produces nothing at
// all before EOF is still a dead stream and must keep failing closed.
func TestOpencodeProcessEventsOpenStepWithoutOutputStillFails(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default(), BuiltinRuntime: true, CLIVersion: "opencode v2.0.28"}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_dead","part":{"type":"step-start"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !result.noTerminalSignal {
		t.Errorf("noTerminalSignal: got false, want true for an empty open step")
	}
}
