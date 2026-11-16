//go:build unix

package agent

import (
	"bytes"
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

// TestCursorExecuteIgnoresUnknownSubtypes pins the fail-safe: a `tool_call` or
// `thinking` event whose subtype we don't recognize (a future non-terminal
// event, or one arriving without a subtype) must be dropped, not coerced into a
// message. Synthesizing a tool_result from a non-terminal subtype would
// decrement the daemon's in-flight tool count early and drop a still-running
// long tool onto the shorter idle watchdog; folding unknown text into reasoning
// corrupts the transcript. Both are the silent-misparse regression this parser
// exists to prevent (MUL-5231 review).
func TestCursorExecuteIgnoresUnknownSubtypes(t *testing.T) {
	t.Parallel()

	// One real tool call (started + completed) bracketing an unknown
	// `progress` subtype and a subtype-less tool_call; one real thinking delta
	// alongside an unknown-subtype thinking event that carries text.
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-unknown"}`,
		`{"type":"thinking","subtype":"delta","text":"real reasoning"}`,
		`{"type":"thinking","subtype":"progress","text":"NOT reasoning"}`,
		`{"type":"thinking","text":"also NOT reasoning"}`,
		`{"type":"tool_call","subtype":"started","call_id":"call-x","tool_call":{"readToolCall":{"args":{"path":"/x"}},"toolCallId":"call-x"}}`,
		`{"type":"tool_call","subtype":"progress","call_id":"call-x","tool_call":{"readToolCall":{"args":{"path":"/x"}},"toolCallId":"call-x"}}`,
		`{"type":"tool_call","call_id":"call-x","tool_call":{"readToolCall":{"args":{"path":"/x"}},"toolCallId":"call-x"}}`,
		`{"type":"tool_call","subtype":"completed","call_id":"call-x","tool_call":{"readToolCall":{"args":{"path":"/x"},"result":{"success":{}}},"toolCallId":"call-x"}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-unknown"}`,
	}
	script := "#!/bin/sh\n"
	for _, line := range lines {
		script += fmt.Sprintf("printf '%%s\\n' %s\n", shellSingleQuote(line))
	}

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

	var thinking strings.Builder
	var toolUses, toolResults int
	for _, msg := range messages {
		switch msg.Type {
		case MessageThinking:
			thinking.WriteString(msg.Content)
		case MessageToolUse:
			toolUses++
		case MessageToolResult:
			toolResults++
		}
	}
	if thinking.String() != "real reasoning" {
		t.Errorf("thinking = %q, want only the delta content %q", thinking.String(), "real reasoning")
	}
	if toolUses != 1 {
		t.Errorf("tool_use count = %d, want 1 (only the started event)", toolUses)
	}
	// Exactly one result: the `completed` event. The `progress` and
	// subtype-less events must not each add a spurious result.
	if toolResults != 1 {
		t.Errorf("tool_result count = %d, want 1 (only the completed event)", toolResults)
	}
}

// shellSingleQuote wraps s in single quotes for POSIX sh, escaping any embedded
// single quote, so a JSON line reaches printf verbatim.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runCursorStreamLines replays the given NDJSON lines through the real cursor
// backend and returns the messages, the result, and everything the backend
// logged.
func runCursorStreamLines(t *testing.T, lines []string) ([]Message, Result, string) {
	t.Helper()

	script := "#!/bin/sh\n"
	for _, line := range lines {
		script += fmt.Sprintf("printf '%%s\\n' %s\n", shellSingleQuote(line))
	}

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: logger})
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

	return messages, result, logBuf.String()
}

func countCursorMessageTypes(messages []Message) map[MessageType]int {
	counts := make(map[MessageType]int)
	for _, msg := range messages {
		counts[msg.Type]++
	}
	return counts
}

// TestCursorExecuteReportsUnhandledTopLevelTypes is the MUL-5434 regression.
//
// Reported symptom: a Cursor run that demonstrably used tools produced a single
// blob of agent text, tools=0, no reasoning — and not one diagnostic. This test
// reproduces that exact stream shape by renaming only the top-level event types
// (`thinking`→`reasoning`, `tool_call`→`tool_calls`) and leaving every nested
// field untouched, which is what an upstream protocol rename looks like.
//
// The parser must still refuse to guess: no tool or reasoning message may be
// synthesized from an event it does not recognize. What it must NOT do is stay
// silent — a silent drop leaves no trace at all, which is what made the original
// report impossible to triage from logs.
//
// This asserts only that the signal exists and that nothing is fabricated. The
// tally is deliberately not treated as a verdict on WHY the transcript is empty;
// see cursorUnhandledTypeTally for the branches it cannot rule out.
func TestCursorExecuteReportsUnhandledTopLevelTypes(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-drift"}`,
		`{"type":"reasoning","subtype":"delta","text":"reasoning under a renamed type"}`,
		`{"type":"tool_calls","subtype":"started","call_id":"call-y","tool_call":{"readToolCall":{"args":{"path":"/y"}},"toolCallId":"call-y"}}`,
		`{"type":"tool_calls","subtype":"completed","call_id":"call-y","tool_call":{"readToolCall":{"args":{"path":"/y"},"result":{"success":{}}},"toolCallId":"call-y"}}`,
		`{"type":"toolCall","subtype":"started","call_id":"call-z","tool_call":{"readToolCall":{"args":{"path":"/z"}},"toolCallId":"call-z"}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"single blob","session_id":"sess-drift"}`,
	}

	messages, result, logs := runCursorStreamLines(t, lines)

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}

	// Unrecognized events are never coerced into transcript rows.
	counts := countCursorMessageTypes(messages)
	if counts[MessageToolUse] != 0 || counts[MessageToolResult] != 0 {
		t.Errorf("unhandled types synthesized tool rows: tool_use=%d tool_result=%d",
			counts[MessageToolUse], counts[MessageToolResult])
	}
	if counts[MessageThinking] != 0 {
		t.Errorf("unhandled type folded into reasoning: thinking=%d", counts[MessageThinking])
	}

	// ...but the drop is now observable, and names the events that were dropped.
	if !strings.Contains(logs, "cursor-agent ignored unhandled event types") {
		t.Fatalf("no unhandled-type warning emitted; logs:\n%s", logs)
	}
	for _, want := range []string{`count=4`, `reasoning=1`, `toolCall=1`, `tool_calls=2`} {
		if !strings.Contains(logs, want) {
			t.Errorf("warning/summary missing %q; logs:\n%s", want, logs)
		}
	}

	// The tally is also on the structured summary line, next to the other
	// protocol counters an operator has to weigh alongside it.
	if !strings.Contains(logs, "unhandled_event_type_count=4") {
		t.Errorf("protocol summary missing unhandled_event_type_count=4; logs:\n%s", logs)
	}
	if !strings.Contains(logs, "tool_use_count=0") {
		t.Errorf("protocol summary missing tool_use_count=0; logs:\n%s", logs)
	}
}

// TestCursorExecuteDoesNotWarnOnHealthyStream guards the other half of the fix:
// the warning has to stay quiet on a run with nothing wrong with it. Top-level
// `user` (the CLI echoing our prompt, present in every recorded run) and the
// `connection` / `retry` control frames carry no transcript content and are
// dropped by design — if those counted as drift the warning would fire on every
// task and stop meaning anything.
func TestCursorExecuteDoesNotWarnOnHealthyStream(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-ok"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"connection","subtype":"reconnecting"}`,
		`{"type":"retry","subtype":"scheduled"}`,
		`{"type":"thinking","subtype":"delta","text":"thinking"}`,
		`{"type":"thinking","subtype":"completed"}`,
		`{"type":"tool_call","subtype":"started","call_id":"call-a","tool_call":{"readToolCall":{"args":{"path":"/a"}},"toolCallId":"call-a"}}`,
		`{"type":"tool_call","subtype":"completed","call_id":"call-a","tool_call":{"readToolCall":{"args":{"path":"/a"},"result":{"success":{}}},"toolCallId":"call-a"}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-ok"}`,
	}

	messages, result, logs := runCursorStreamLines(t, lines)

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if strings.Contains(logs, "cursor-agent ignored unhandled event types") {
		t.Errorf("unhandled-type warning fired on a healthy stream; logs:\n%s", logs)
	}
	if !strings.Contains(logs, "unhandled_event_type_count=0") {
		t.Errorf("protocol summary should report unhandled_event_type_count=0; logs:\n%s", logs)
	}
	// Suppressing the warning must not have suppressed the real rows: the
	// known-good `thinking` / `tool_call` events still reach the transcript.
	counts := countCursorMessageTypes(messages)
	if counts[MessageToolUse] != 1 || counts[MessageToolResult] != 1 || counts[MessageThinking] != 1 {
		t.Errorf("healthy stream lost rows: tool_use=%d tool_result=%d thinking=%d",
			counts[MessageToolUse], counts[MessageToolResult], counts[MessageThinking])
	}
}

// TestCursorLastAssistantBytesExcludesResultText pins the second half of
// MUL-5434's diagnostic gap. The result event writes its text into the same
// builder as assistant text, so the summary used to report
// last_assistant_bytes == result_bytes even when the assistant streamed
// nothing — erasing the one signal that says "no incremental output arrived,
// only the final answer", which is precisely the reported failure.
func TestCursorLastAssistantBytesExcludesResultText(t *testing.T) {
	t.Parallel()

	t.Run("result only", func(t *testing.T) {
		t.Parallel()
		_, result, logs := runCursorStreamLines(t, []string{
			`{"type":"system","subtype":"init","session_id":"sess-blob"}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"0123456789","session_id":"sess-blob"}`,
		})
		if result.Status != "completed" {
			t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
		}
		if !strings.Contains(logs, "result_bytes=10") {
			t.Errorf("summary missing result_bytes=10; logs:\n%s", logs)
		}
		if !strings.Contains(logs, "last_assistant_bytes=0") {
			t.Errorf("assistant streamed nothing, so last_assistant_bytes must be 0; logs:\n%s", logs)
		}
	})

	t.Run("assistant text counted", func(t *testing.T) {
		t.Parallel()
		_, result, logs := runCursorStreamLines(t, []string{
			`{"type":"system","subtype":"init","session_id":"sess-text"}`,
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"abcde"}]}}`,
			`{"type":"result","subtype":"success","is_error":false,"result":"0123456789","session_id":"sess-text"}`,
		})
		if result.Status != "completed" {
			t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
		}
		// The result text is NOT appended (output is already non-empty), and the
		// assistant's own 5 bytes are what gets reported.
		if !strings.Contains(logs, "last_assistant_bytes=5") {
			t.Errorf("summary should report the assistant's 5 bytes; logs:\n%s", logs)
		}
		if result.Output != "abcde" {
			t.Errorf("output = %q, want %q", result.Output, "abcde")
		}
	})
}
