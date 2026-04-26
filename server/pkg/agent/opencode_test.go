package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewReturnsOpencodeBackend(t *testing.T) {
	t.Parallel()
	b, err := New("opencode", Config{ExecutablePath: "/nonexistent/opencode"})
	if err != nil {
		t.Fatalf("New(opencode) error: %v", err)
	}
	if _, ok := b.(*opencodeBackend); !ok {
		t.Fatalf("expected *opencodeBackend, got %T", b)
	}
}

func TestResolveOpenCodeWindowsNativeBinaryPrefersNativeExe(t *testing.T) {
	t.Parallel()

	shimDir := t.TempDir()
	native := filepath.Join(shimDir, "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("fake exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveOpenCodeWindowsNativeBinary(filepath.Join(shimDir, "opencode.cmd"))
	if got != native {
		t.Fatalf("native path: got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeWindowsNativeBinaryFallsBackToBaseline(t *testing.T) {
	t.Parallel()

	shimDir := t.TempDir()
	native := filepath.Join(shimDir, "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64-baseline", "bin", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("fake exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveOpenCodeWindowsNativeBinary(filepath.Join(shimDir, "opencode.cmd"))
	if got != native {
		t.Fatalf("native path: got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeWindowsNativeBinaryFromPackageBin(t *testing.T) {
	t.Parallel()

	packageRoot := filepath.Join(t.TempDir(), "node_modules", "opencode-ai")
	native := filepath.Join(packageRoot, "node_modules", "opencode-windows-x64", "bin", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("fake exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveOpenCodeWindowsNativeBinary(filepath.Join(packageRoot, "bin", "opencode"))
	if got != native {
		t.Fatalf("native path: got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeExecPathWindowsPrefersNativeBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific executable substitution")
	}

	shimDir := t.TempDir()
	shim := filepath.Join(shimDir, "opencode.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(shimDir, "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")
	if err := os.MkdirAll(filepath.Dir(native), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(native, []byte("fake exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveOpenCodeExecPath(shim)
	if err != nil {
		t.Fatalf("resolve executable: %v", err)
	}
	if got != native {
		t.Fatalf("exec path: got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeExecPathFallsBackToResolvedExecutable(t *testing.T) {
	t.Parallel()

	execPath := writeOpenCodeTestExecutable(t)
	got, err := resolveOpenCodeExecPath(execPath)
	if err != nil {
		t.Fatalf("resolve executable: %v", err)
	}
	if got != execPath {
		t.Fatalf("exec path: got %q, want %q", got, execPath)
	}
}

func writeOpenCodeTestExecutable(tb testing.TB) string {
	tb.Helper()

	dir := tb.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "opencode.cmd")
		if err := os.WriteFile(path, []byte("@echo off\r\n"), 0o755); err != nil {
			tb.Fatal(err)
		}
		return path
	}

	path := filepath.Join(dir, "opencode")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		tb.Fatal(err)
	}
	return path
}

// ── Text event tests ──

func TestOpencodeHandleTextEvent(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)
	var output strings.Builder

	event := opencodeEvent{
		Type:      "text",
		SessionID: "ses_abc",
		Part: opencodeEventPart{
			Type: "text",
			Text: "Hello from opencode",
		},
	}

	b.handleTextEvent(event, ch, &output)

	if output.String() != "Hello from opencode" {
		t.Errorf("output: got %q, want %q", output.String(), "Hello from opencode")
	}
	msg := <-ch
	if msg.Type != MessageText {
		t.Errorf("type: got %v, want MessageText", msg.Type)
	}
	if msg.Content != "Hello from opencode" {
		t.Errorf("content: got %q, want %q", msg.Content, "Hello from opencode")
	}
}

func TestOpencodeHandleTextEventEmpty(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)
	var output strings.Builder

	event := opencodeEvent{
		Type: "text",
		Part: opencodeEventPart{Type: "text", Text: ""},
	}

	b.handleTextEvent(event, ch, &output)

	if output.String() != "" {
		t.Errorf("expected empty output, got %q", output.String())
	}
	if len(ch) != 0 {
		t.Errorf("expected no messages, got %d", len(ch))
	}
}

// ── Tool use event tests (real opencode schema) ──

func TestOpencodeHandleToolUseEventCompleted(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)

	// Real opencode tool_use event: single event with state containing both
	// call parameters and result.
	event := opencodeEvent{
		Type: "tool_use",
		Part: opencodeEventPart{
			Tool:   "bash",
			CallID: "call_BHA1",
			State: &opencodeToolState{
				Status: "completed",
				Input:  json.RawMessage(`{"command":"pwd","description":"Prints current working directory path"}`),
				Output: "/tmp/multica\n",
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	// Should emit both a tool-use and a tool-result message.
	if len(ch) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ch))
	}

	// First: tool-use
	msg := <-ch
	if msg.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", msg.Type)
	}
	if msg.Tool != "bash" {
		t.Errorf("tool: got %q, want %q", msg.Tool, "bash")
	}
	if msg.CallID != "call_BHA1" {
		t.Errorf("callID: got %q, want %q", msg.CallID, "call_BHA1")
	}
	if cmd, ok := msg.Input["command"].(string); !ok || cmd != "pwd" {
		t.Errorf("input.command: got %v", msg.Input["command"])
	}

	// Second: tool-result
	msg = <-ch
	if msg.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", msg.Type)
	}
	if msg.CallID != "call_BHA1" {
		t.Errorf("callID: got %q, want %q", msg.CallID, "call_BHA1")
	}
	if msg.Output != "/tmp/multica\n" {
		t.Errorf("output: got %q", msg.Output)
	}
}

func TestOpencodeHandleToolUseEventPending(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)

	// Tool use with pending status — only emit tool-use, no result.
	event := opencodeEvent{
		Type: "tool_use",
		Part: opencodeEventPart{
			Tool:   "read",
			CallID: "call_ABC",
			State: &opencodeToolState{
				Status: "pending",
				Input:  json.RawMessage(`{"filePath":"/tmp/test.go"}`),
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 1 {
		t.Fatalf("expected 1 message for pending tool, got %d", len(ch))
	}
	msg := <-ch
	if msg.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", msg.Type)
	}
	if msg.Tool != "read" {
		t.Errorf("tool: got %q, want %q", msg.Tool, "read")
	}
}

func TestOpencodeHandleToolUseEventStructuredOutput(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)

	// Tool with structured (non-string) output.
	event := opencodeEvent{
		Type: "tool_use",
		Part: opencodeEventPart{
			Tool:   "glob",
			CallID: "call_XYZ",
			State: &opencodeToolState{
				Status: "completed",
				Input:  json.RawMessage(`{"pattern":"*.go"}`),
				Output: map[string]any{"files": []any{"main.go", "main_test.go"}},
			},
		},
	}

	b.handleToolUseEvent(event, ch)

	// tool-use + tool-result
	if len(ch) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ch))
	}
	<-ch // skip tool-use
	msg := <-ch
	if msg.Type != MessageToolResult {
		t.Errorf("type: got %v, want MessageToolResult", msg.Type)
	}
	if !strings.Contains(msg.Output, "main.go") {
		t.Errorf("output should contain 'main.go', got %q", msg.Output)
	}
}

func TestOpencodeHandleToolUseEventNilState(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{}
	ch := make(chan Message, 10)

	// Tool use with no state at all — should emit tool-use with no crash.
	event := opencodeEvent{
		Type: "tool_use",
		Part: opencodeEventPart{
			Tool:   "write",
			CallID: "call_NUL",
		},
	}

	b.handleToolUseEvent(event, ch)

	if len(ch) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ch))
	}
	msg := <-ch
	if msg.Type != MessageToolUse {
		t.Errorf("type: got %v, want MessageToolUse", msg.Type)
	}
}

// ── Error event tests ──

func TestOpencodeHandleErrorEvent(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	event := opencodeEvent{
		Type:      "error",
		SessionID: "ses_abc",
		Error: &opencodeError{
			Name: "UnknownError",
			Data: &opencodeErrData{
				Message: "Model not found: definitely/not-a-model.",
			},
		},
	}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if status != "failed" {
		t.Errorf("status: got %q, want %q", status, "failed")
	}
	if errMsg != "Model not found: definitely/not-a-model." {
		t.Errorf("error: got %q", errMsg)
	}
	msg := <-ch
	if msg.Type != MessageError {
		t.Errorf("type: got %v, want MessageError", msg.Type)
	}
	if msg.Content != "Model not found: definitely/not-a-model." {
		t.Errorf("content: got %q", msg.Content)
	}
}

func TestOpencodeHandleErrorEventNameOnly(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	// Error with name but no data.message — should fall back to name.
	event := opencodeEvent{
		Type: "error",
		Error: &opencodeError{
			Name: "RateLimitError",
		},
	}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if errMsg != "RateLimitError" {
		t.Errorf("error: got %q, want %q", errMsg, "RateLimitError")
	}
}

func TestOpencodeHandleErrorEventNilError(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	event := opencodeEvent{Type: "error"}

	b.handleErrorEvent(event, ch, &status, &errMsg)

	if errMsg != "unknown opencode error" {
		t.Errorf("error: got %q, want %q", errMsg, "unknown opencode error")
	}
}

// ── JSON parsing tests with real fixtures ──

func TestOpencodeEventParsingTextFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"text","timestamp":1775116675833,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","type":"text","text":"pong","time":{"start":1775116675833,"end":1775116675833}}}`

	var event opencodeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "text" {
		t.Errorf("type: got %q, want %q", event.Type, "text")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q, want %q", event.SessionID, "ses_abc")
	}
	if event.Part.Text != "pong" {
		t.Errorf("part.text: got %q, want %q", event.Part.Text, "pong")
	}
}

func TestOpencodeEventParsingToolUseFixture(t *testing.T) {
	t.Parallel()

	// Real `tool_use` JSON from live `opencode run --format json` output.
	line := `{"type":"tool_use","timestamp":1775117187163,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","type":"tool","tool":"bash","callID":"call_BHA1","state":{"status":"completed","input":{"command":"pwd","description":"Prints current working directory path"},"output":"/tmp/multica\n","metadata":{"exit":0},"time":{"start":1775117187092,"end":1775117187162}}}}`

	var event opencodeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "tool_use" {
		t.Errorf("type: got %q, want %q", event.Type, "tool_use")
	}
	if event.Part.Tool != "bash" {
		t.Errorf("part.tool: got %q, want %q", event.Part.Tool, "bash")
	}
	if event.Part.CallID != "call_BHA1" {
		t.Errorf("part.callID: got %q, want %q", event.Part.CallID, "call_BHA1")
	}
	if event.Part.State == nil {
		t.Fatal("part.state is nil")
	}
	if event.Part.State.Status != "completed" {
		t.Errorf("state.status: got %q, want %q", event.Part.State.Status, "completed")
	}

	// Parse state.input
	var input map[string]any
	if err := json.Unmarshal(event.Part.State.Input, &input); err != nil {
		t.Fatalf("unmarshal state.input: %v", err)
	}
	if input["command"] != "pwd" {
		t.Errorf("state.input.command: got %v, want %q", input["command"], "pwd")
	}

	// state.output should be a string
	if output, ok := event.Part.State.Output.(string); !ok || output != "/tmp/multica\n" {
		t.Errorf("state.output: got %v (%T)", event.Part.State.Output, event.Part.State.Output)
	}
}

func TestOpencodeEventParsingErrorFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"error","timestamp":1775117233612,"sessionID":"ses_abc","error":{"name":"UnknownError","data":{"message":"Model not found: definitely/not-a-model."}}}`

	var event opencodeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "error" {
		t.Errorf("type: got %q, want %q", event.Type, "error")
	}
	if event.Error == nil {
		t.Fatal("error field is nil")
	}
	if event.Error.Name != "UnknownError" {
		t.Errorf("error.name: got %q", event.Error.Name)
	}
	if got := event.Error.Message(); got != "Model not found: definitely/not-a-model." {
		t.Errorf("error.Message(): got %q", got)
	}
}

func TestOpencodeEventParsingStepStartFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"step_start","timestamp":1775116675819,"sessionID":"ses_abc","part":{"id":"prt_123","messageID":"msg_456","sessionID":"ses_abc","snapshot":"abc123","type":"step-start"}}`

	var event opencodeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "step_start" {
		t.Errorf("type: got %q, want %q", event.Type, "step_start")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q", event.SessionID)
	}
}

func TestOpencodeStepFinishParsing(t *testing.T) {
	t.Parallel()

	line := `{"type":"step_finish","timestamp":1775116676180,"sessionID":"ses_abc","part":{"id":"prt_789","reason":"stop","snapshot":"abc123","messageID":"msg_456","sessionID":"ses_abc","type":"step-finish","tokens":{"total":14674,"input":14585,"output":89,"reasoning":82,"cache":{"write":0,"read":0}},"cost":0}}`

	var event opencodeEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "step_finish" {
		t.Errorf("type: got %q, want %q", event.Type, "step_finish")
	}
	if event.SessionID != "ses_abc" {
		t.Errorf("sessionID: got %q", event.SessionID)
	}
}

// ── extractToolOutput tests ──

func TestExtractToolOutputString(t *testing.T) {
	t.Parallel()
	if got := extractToolOutput("hello\n"); got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

func TestExtractToolOutputNil(t *testing.T) {
	t.Parallel()
	if got := extractToolOutput(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractToolOutputStructured(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"key": "value"}
	got := extractToolOutput(obj)
	if !strings.Contains(got, `"key"`) || !strings.Contains(got, `"value"`) {
		t.Errorf("got %q, expected JSON containing key/value", got)
	}
}

// ── opencodeError.Message() tests ──

func TestOpencodeErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *opencodeError
		want string
	}{
		{
			name: "data message",
			err:  &opencodeError{Name: "Err", Data: &opencodeErrData{Message: "details"}},
			want: "details",
		},
		{
			name: "name only",
			err:  &opencodeError{Name: "RateLimitError"},
			want: "RateLimitError",
		},
		{
			name: "empty",
			err:  &opencodeError{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Message(); got != tt.want {
				t.Errorf("Message() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── Integration-level tests: processEvents ──
//
// These feed multiple JSON lines through processEvents and verify the
// accumulated result (status, output, sessionID, error) and emitted messages.

func TestOpencodeProcessEventsHappyPath(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Simulate a successful run: step_start → text → tool_use → text → step_finish
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_happy","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_happy","part":{"type":"text","text":"Analyzing the issue..."}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_happy","part":{"tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"ls"},"output":"file1.go\nfile2.go\n"}}}`,
		`{"type":"text","timestamp":1003,"sessionID":"ses_happy","part":{"type":"text","text":" Done."}}`,
		`{"type":"step_finish","timestamp":1004,"sessionID":"ses_happy","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	// Verify result.
	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.sessionID != "ses_happy" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_happy")
	}
	if result.output != "Analyzing the issue... Done." {
		t.Errorf("output: got %q, want %q", result.output, "Analyzing the issue... Done.")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	// Drain and verify messages.
	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}

	// Expected: status(running), text, tool-use, tool-result, text, = 5 messages
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Type != MessageStatus || msgs[0].Status != "running" {
		t.Errorf("msg[0]: got %+v, want status=running", msgs[0])
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "Analyzing the issue..." {
		t.Errorf("msg[1]: got %+v", msgs[1])
	}
	if msgs[2].Type != MessageToolUse || msgs[2].Tool != "bash" {
		t.Errorf("msg[2]: got %+v, want tool-use(bash)", msgs[2])
	}
	if msgs[3].Type != MessageToolResult || msgs[3].Output != "file1.go\nfile2.go\n" {
		t.Errorf("msg[3]: got %+v, want tool-result", msgs[3])
	}
	if msgs[4].Type != MessageText || msgs[4].Content != " Done." {
		t.Errorf("msg[4]: got %+v", msgs[4])
	}
}

func TestOpencodeProcessEventsErrorCausesFailedStatus(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Simulate: step_start → error (model not found) → step_finish.
	// OpenCode exits RC=0 on error events, so the error event is the only
	// signal that something went wrong.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_err","part":{"type":"step-start"}}`,
		`{"type":"error","timestamp":1001,"sessionID":"ses_err","error":{"name":"UnknownError","data":{"message":"Model not found: bad/model"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_err","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if result.errMsg != "Model not found: bad/model" {
		t.Errorf("errMsg: got %q", result.errMsg)
	}
	if result.sessionID != "ses_err" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_err")
	}

	close(ch)
	var errorMsgs int
	for m := range ch {
		if m.Type == MessageError {
			errorMsgs++
		}
	}
	if errorMsgs != 1 {
		t.Errorf("expected 1 error message, got %d", errorMsgs)
	}
}

func TestOpencodeProcessEventsSessionIDExtracted(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Session ID should be captured from the last event that has one.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_first","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_updated","part":{"type":"text","text":"hi"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.sessionID != "ses_updated" {
		t.Errorf("sessionID: got %q, want %q (should use last seen)", result.sessionID, "ses_updated")
	}

	close(ch)
}

func TestOpencodeProcessEventsScannerError(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Use an ioErrReader that returns valid data then an I/O error, which
	// triggers scanner.Err() and should set status to "failed".
	result := b.processEvents(&ioErrReader{
		data: `{"type":"text","sessionID":"ses_scan","part":{"text":"before error"}}` + "\n",
	}, ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "stdout read error") {
		t.Errorf("errMsg: got %q, want it to contain 'stdout read error'", result.errMsg)
	}
	// The text event before the error should still be captured.
	if result.output != "before error" {
		t.Errorf("output: got %q, want %q", result.output, "before error")
	}

	close(ch)
}

// ioErrReader delivers data on the first Read, then returns an error on the second.
type ioErrReader struct {
	data string
	read bool
}

func (r *ioErrReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.data)
		return n, nil
	}
	return 0, fmt.Errorf("simulated I/O error")
}

func TestOpencodeProcessEventsEmptyLines(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Empty lines and invalid JSON should be skipped without error.
	lines := strings.Join([]string{
		"",
		"   ",
		"not json at all",
		`{"type":"text","sessionID":"ses_ok","part":{"text":"valid"}}`,
		"",
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.output != "valid" {
		t.Errorf("output: got %q, want %q", result.output, "valid")
	}
	if result.sessionID != "ses_ok" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_ok")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Type != MessageText {
		t.Errorf("expected 1 text message, got %d: %+v", len(msgs), msgs)
	}
}

func TestOpencodeProcessEventsErrorDoesNotRevertToCompleted(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Error event followed by more text — status should remain "failed".
	lines := strings.Join([]string{
		`{"type":"error","sessionID":"ses_x","error":{"name":"RateLimitError"}}`,
		`{"type":"text","sessionID":"ses_x","part":{"text":"recovered?"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q (error should stick)", result.status, "failed")
	}
	if result.errMsg != "RateLimitError" {
		t.Errorf("errMsg: got %q, want %q", result.errMsg, "RateLimitError")
	}

	close(ch)
}
