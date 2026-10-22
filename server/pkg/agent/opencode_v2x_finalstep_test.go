package agent

import (
	"log/slog"
	"strings"
	"testing"
)

// The OpenCode 2.1.x stream vocabulary.
//
// Captured verbatim from a real run against opencode v2.1.2 (the CLI the
// OpenCode desktop distribution ships) with the local Strata/Tiel model:
//
//	step_start -> tool_use -> step_finish(reason=tool-calls) -> step_start -> text
//
// The last step is never closed: 2.1.x ends a final text-only step by ending
// the stream instead of emitting step_finish for it. The 2.0.x contract this
// parser was written against always closed the last step with step_finish,
// reason "stop" — so against 2.1.x the EOF check saw an open step and failed
// every run, including runs whose work was complete and correct.
const v21StepStart = `{"type":"step_start","timestamp":1792634342037,"sessionID":"ses_v21","part":{"id":"prt_a","sessionID":"ses_v21","messageID":"msg_a","type":"step-start"}}`

const v21ToolUse = `{"type":"tool_use","timestamp":1792634343253,"sessionID":"ses_v21","part":{"partID":"prt_b","sessionID":"ses_v21","messageID":"msg_a","type":"tool","id":"call_1","tool":"write","state":{"status":"completed","input":{"path":"probe.txt","content":"HELLO"},"output":"Created file successfully: probe.txt","title":"write","metadata":{"providerCall":{"executed":false},"providerResult":{"executed":false}}}}}`

const v21StepFinishToolCalls = `{"type":"step_finish","timestamp":1792634343255,"sessionID":"ses_v21","part":{"id":"prt_c","sessionID":"ses_v21","messageID":"msg_a","type":"step-finish","reason":"tool-calls","cost":0,"tokens":{"input":10585,"output":69,"reasoning":0,"cache":{"read":0,"write":0}}}}`

const v21FinalStepStart = `{"type":"step_start","timestamp":1792634343607,"sessionID":"ses_v21","part":{"id":"prt_d","sessionID":"ses_v21","messageID":"msg_b","type":"step-start"}}`

const v21FinalText = `{"type":"text","timestamp":1792634343772,"sessionID":"ses_v21","part":{"id":"prt_d_text-0","sessionID":"ses_v21","messageID":"msg_b","type":"text","text":"DONE","time":{"start":1792634343281,"end":1792634343772}}}`

// TestOpencodeProcessEventsV21FinalTextStepCompletes is the regression test
// for the false-negative verdict: a 2.1.x run that produced its final answer
// must be recorded as completed, not as "stream ended without a terminal
// signal".
func TestOpencodeProcessEventsV21FinalTextStepCompletes(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		v21StepStart,
		v21ToolUse,
		v21StepFinishToolCalls,
		v21FinalStepStart,
		v21FinalText,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Fatalf("status: got %q, want %q (errMsg=%q)", result.status, "completed", result.errMsg)
	}
	if result.noTerminalSignal {
		t.Error("noTerminalSignal: got true, want false — the run produced its final answer")
	}
	if !result.sawTerminalSignal {
		t.Error("sawTerminalSignal: got false, want true")
	}
	if !strings.Contains(result.output, "DONE") {
		t.Errorf("output: got %q, want it to contain %q", result.output, "DONE")
	}
	if result.usage.InputTokens != 10585 || result.usage.OutputTokens != 69 {
		t.Errorf("usage: got %+v, want input=10585 output=69", result.usage)
	}
	if result.sessionID != "ses_v21" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_v21")
	}
}

// TestOpencodeProcessEventsV21TextOnlyStreamCompletes covers the same shape
// without the tool step: a run whose only step is text.
func TestOpencodeProcessEventsV21TextOnlyStreamCompletes(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{v21StepStart, v21FinalText}, "\n")
	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Fatalf("status: got %q, want %q (errMsg=%q)", result.status, "completed", result.errMsg)
	}
	if result.noTerminalSignal {
		t.Error("noTerminalSignal: got true, want false")
	}
	if !result.sawTerminalSignal {
		t.Error("sawTerminalSignal: got false, want true")
	}
}

// TestOpencodeProcessEventsOpenStepWithoutOutputFailsClosed pins the
// fail-closed behaviour that must survive the fix: a step that produced
// nothing at all still means the provider died before answering.
func TestOpencodeProcessEventsOpenStepWithoutOutputFailsClosed(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(v21StepStart+"\n"), ch)

	if result.status != "failed" {
		t.Fatalf("status: got %q, want %q", result.status, "failed")
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}
	if !strings.Contains(result.errMsg, "step still open at EOF") {
		t.Errorf("errMsg: got %q, want it to mention the open step", result.errMsg)
	}
}

// TestOpencodeProcessEventsOpenStepWithPendingToolFailsClosed pins the other
// fail-closed shape: a step whose tool result was never fed back. The run did
// not finish, so it must not be reported as completed even though the step
// emitted a tool call.
func TestOpencodeProcessEventsOpenStepWithPendingToolFailsClosed(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{v21StepStart, v21ToolUse}, "\n")
	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Fatalf("status: got %q, want %q", result.status, "failed")
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}
}

// TestOpencodeProcessEventsUnfinishedTextPartFailsClosed is the sharp edge of
// the completion-window criterion: the step emitted text, but the part never
// got its end timestamp — the provider died inside the part. The stream shape
// is otherwise identical to a finished 2.1.x run, so only the window tells
// them apart, and this one must still fail closed.
func TestOpencodeProcessEventsUnfinishedTextPartFailsClosed(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	unfinishedText := `{"type":"text","timestamp":1792634343772,"sessionID":"ses_v21","part":{"id":"prt_d_text-0","sessionID":"ses_v21","messageID":"msg_b","type":"text","text":"DONE","time":{"start":1792634343281}}}`

	lines := strings.Join([]string{v21StepStart, unfinishedText}, "\n")
	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Fatalf("status: got %q, want %q", result.status, "failed")
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}
	if !strings.Contains(result.errMsg, "step still open at EOF") {
		t.Errorf("errMsg: got %q, want it to mention the open step", result.errMsg)
	}
}
