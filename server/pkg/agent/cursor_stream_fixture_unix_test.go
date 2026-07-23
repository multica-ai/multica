//go:build unix

package agent

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCursorExecuteParsesRecordedStream replays a stream captured from a real
// `cursor-agent -p --output-format stream-json --yolo` run (2026.07.20-8cc9c0b,
// the exact invocation the daemon uses) and pins every observable step of it.
//
// The regression it guards: reasoning and tool calls arrive as TOP-LEVEL
// `thinking` / `tool_call` events, not as content blocks inside assistant
// messages. When the parser only understood the assistant-block shape it
// dropped all of them silently and the task transcript showed a single step
// (MUL-5231). A fixture test is what makes the next upstream event rename loud
// instead of silent.
func TestCursorExecuteParsesRecordedStream(t *testing.T) {
	t.Parallel()

	fixture, err := filepath.Abs(filepath.Join("testdata", "cursor-agent-2026.07.20-stream-json.jsonl"))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec cat %q\n", fixture)

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()
	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}

	// Reasoning: three blocks streamed as nine deltas, kept in order and
	// separated by a blank line so the daemon's concatenation stays readable.
	const wantThinking = "Reading notes.txt.\n\nRunning wc -l on notes.txt. Writing the line count to count.txt." +
		"\n\nnotes.txt contains 3 lines. I will write 3 into count.txt." +
		"\n\nFinished. The line count was written to count.txt."
	var gotThinking strings.Builder
	for _, msg := range messages {
		if msg.Type == MessageThinking {
			gotThinking.WriteString(msg.Content)
		}
	}
	if gotThinking.String() != wantThinking {
		t.Errorf("thinking =\n%q\nwant\n%q", gotThinking.String(), wantThinking)
	}

	// Tool calls: name comes from the nested `<name>ToolCall` key, arguments
	// from that key's `args`, and the call id is normalized to a single line.
	wantUses := []struct {
		tool     string
		callID   string
		inputKey string
		inputVal string
	}{
		{tool: "read", callID: "call-bb11656a-e59e-4356-9866-5b206aedb390-0", inputKey: "path", inputVal: "/tmp/curcap/notes.txt"},
		{tool: "shell", callID: "call-bb11656a-e59e-4356-9866-5b206aedb390-1", inputKey: "command", inputVal: "wc -l notes.txt"},
		{tool: "edit", callID: "call-c52c0cd6-81ad-4a87-94c5-f5b0119f3ed4-2", inputKey: "path", inputVal: "/tmp/curcap/count.txt"},
	}
	var uses []Message
	for _, msg := range messages {
		if msg.Type == MessageToolUse {
			uses = append(uses, msg)
		}
	}
	if len(uses) != len(wantUses) {
		t.Fatalf("tool_use count = %d, want %d (%+v)", len(uses), len(wantUses), uses)
	}
	for i, want := range wantUses {
		got := uses[i]
		if got.Tool != want.tool {
			t.Errorf("tool_use[%d].Tool = %q, want %q", i, got.Tool, want.tool)
		}
		if got.CallID != want.callID {
			t.Errorf("tool_use[%d].CallID = %q, want %q", i, got.CallID, want.callID)
		}
		if fmt.Sprint(got.Input[want.inputKey]) != want.inputVal {
			t.Errorf("tool_use[%d].Input[%q] = %v, want %q", i, want.inputKey, got.Input[want.inputKey], want.inputVal)
		}
	}

	// Results pair with their call by id, carry the tool name directly, and
	// keep the tool's own result payload as output.
	wantResults := []struct {
		tool     string
		callID   string
		contains string
	}{
		{tool: "read", callID: "call-bb11656a-e59e-4356-9866-5b206aedb390-0", contains: "alpha"},
		{tool: "shell", callID: "call-bb11656a-e59e-4356-9866-5b206aedb390-1", contains: "3 notes.txt"},
		{tool: "edit", callID: "call-c52c0cd6-81ad-4a87-94c5-f5b0119f3ed4-2", contains: "Wrote contents to /tmp/curcap/count.txt"},
	}
	var results []Message
	for _, msg := range messages {
		if msg.Type == MessageToolResult {
			results = append(results, msg)
		}
	}
	if len(results) != len(wantResults) {
		t.Fatalf("tool_result count = %d, want %d (%+v)", len(results), len(wantResults), results)
	}
	for i, want := range wantResults {
		got := results[i]
		if got.Tool != want.tool {
			t.Errorf("tool_result[%d].Tool = %q, want %q", i, got.Tool, want.tool)
		}
		if got.CallID != want.callID {
			t.Errorf("tool_result[%d].CallID = %q, want %q", i, got.CallID, want.callID)
		}
		if !strings.Contains(got.Output, want.contains) {
			t.Errorf("tool_result[%d].Output = %q, want it to contain %q", i, got.Output, want.contains)
		}
	}

	// A result must never precede its own tool_use, otherwise the daemon's
	// in-flight tool counter goes negative and re-arms the watchdog early.
	seen := map[string]bool{}
	for _, msg := range messages {
		switch msg.Type {
		case MessageToolUse:
			seen[msg.CallID] = true
		case MessageToolResult:
			if !seen[msg.CallID] {
				t.Errorf("tool_result for %q arrived before its tool_use", msg.CallID)
			}
		}
	}

	const wantText = "I'll read `notes.txt`, run `wc -l`, then write the line count to `count.txt`." +
		"`notes.txt` has 3 lines (`alpha`, `beta`, `gamma`). Wrote `3` to `count.txt`."
	if result.Output != wantText {
		t.Errorf("result output = %q, want %q", result.Output, wantText)
	}
	if result.SessionID == "" {
		t.Error("session id not captured from the recorded stream")
	}
}
