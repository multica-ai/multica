package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestOpencodeProcessEventsStreamEndsMidStep(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Regression for production run B: the provider stream died right after a
	// mid-step planning message. opencode reaches a clean EOF and exits 0 with
	// the step still open (no step_finish, no error event), which previously
	// reported "completed" — a false-green with all work lost.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_midstep","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_midstep","part":{"type":"text","text":"Let me plan the work..."}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}
	// Text emitted before the stream died must still be captured.
	if result.output != "Let me plan the work..." {
		t.Errorf("output: got %q", result.output)
	}

	close(ch)
}

func TestOpencodeProcessEventsStreamEndsAfterToolBeforeStepFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Regression for production run A: the stream died after a tool ran but
	// before the step closed. opencode emits tool_use only on terminal states
	// (here completed), so unfinished work manifests as an unclosed step, not a
	// pending tool — the open step at EOF is what fails the run.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_tool","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}

	close(ch)
}

func TestOpencodeProcessEventsMultiStepHappyPath(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// A full tool loop as it appears on the real wire (live-probed against
	// opencode 1.17.16): the tool step closes with reason "tool-calls", the
	// continuation step opens and closes with the final reason "stop". Must
	// stay "completed" — "stop" is the run-level terminal signal.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_multi","part":{"type":"text","text":"step one"}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_multi","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"echo ok"},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1003,"sessionID":"ses_multi","part":{"type":"step-finish","reason":"tool-calls"}}`,
		`{"type":"step_start","timestamp":1004,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1005,"sessionID":"ses_multi","part":{"type":"text","text":" step two"}}`,
		`{"type":"step_finish","timestamp":1006,"sessionID":"ses_multi","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.output != "step one step two" {
		t.Errorf("output: got %q", result.output)
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestOpencodeProcessEventsStreamEndsAfterToolCallsStepFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// A step_finish with reason "tool-calls" is not a run-level terminal
	// signal: tool results still have to be fed back in a continuation step.
	// If the stream dies in the gap between that finish and the next
	// step_start, the run did not complete — even though no step is open.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_toolcalls","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_toolcalls","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_toolcalls","part":{"type":"step-finish","reason":"tool-calls"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}

	close(ch)
}

func TestOpencodeProcessEventsStreamEndsAfterToolWithStopFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Some providers return reason "stop" even when the assistant step contains
	// tool calls. OpenCode still continues the run so it can feed the tool result
	// back to the model. EOF before that continuation step is therefore not a
	// successful run-level terminal signal.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_stop_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_stop_tool","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"go test ./..."},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_stop_tool","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if !strings.Contains(result.errMsg, "terminal signal") {
		t.Errorf("errMsg: got %q, want it to mention the missing terminal signal", result.errMsg)
	}
	if !result.noTerminalSignal {
		t.Error("noTerminalSignal: got false, want true")
	}

	close(ch)
}

func TestOpencodeProcessEventsToolWithStopFinishThenContinuation(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// The provider-specific "stop" quirk must not false-fail a healthy run once
	// OpenCode starts the continuation step and reaches a real terminal finish.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_stop_continue","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_stop_continue","part":{"type":"tool","tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"echo ok"},"output":"ok\n"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_stop_continue","part":{"type":"step-finish","reason":"stop"}}`,
		`{"type":"step_start","timestamp":1003,"sessionID":"ses_stop_continue","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1004,"sessionID":"ses_stop_continue","part":{"type":"text","text":"done"}}`,
		`{"type":"step_finish","timestamp":1005,"sessionID":"ses_stop_continue","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestOpencodeProcessEventsProviderExecutedToolWithStopFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Provider-executed tools do not require OpenCode to feed a local tool
	// result back to the model, so a "stop" finish remains terminal.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_provider_tool","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_provider_tool","part":{"type":"tool","tool":"web_search","callID":"call_1","metadata":{"providerExecuted":true},"state":{"status":"completed","input":{"query":"weather"},"output":"sunny"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_provider_tool","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestOpencodeProcessEventsStepFinishWithoutReasonBackcompat(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// Older opencode versions emit step-finish parts without a reason field.
	// A missing (or unknown) reason must be treated as terminal so healthy
	// runs on old protocols are not mass-failed; only an explicit
	// "tool-calls" keeps the run non-terminal.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_noreason","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_noreason","part":{"type":"text","text":"legacy run"}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_noreason","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

func TestOpencodeProcessEventsToolErrorThenCleanFinish(t *testing.T) {
	t.Parallel()

	b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	// A recovered tool error is normal in a healthy run: opencode emits tool_use
	// only on terminal states, and a failed tool arrives with state.status=="error"
	// (real wire shape from `opencode run --format json`). The run continues and
	// closes every step with step_finish, so status must stay "completed". The
	// earlier pending-tool heuristic recorded the error tool's callID, never
	// drained it, and wrongly reported this healthy run as failed.
	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_toolerr","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":1001,"sessionID":"ses_toolerr","part":{"type":"tool","tool":"read","callID":"functions.read:1","state":{"status":"error","input":{"filePath":"/nonexistent-path-xyz/also-missing.md"},"error":"File not found: /nonexistent-path-xyz/also-missing.md"}}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_toolerr","part":{"type":"tool","tool":"bash","callID":"functions.bash:0","state":{"status":"completed","input":{"command":"echo hi"},"output":"hi\n"}}}`,
		`{"type":"step_finish","timestamp":1003,"sessionID":"ses_toolerr","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q (recovered tool error must not fail a healthy run)", result.status, "completed")
	}
	if result.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", result.errMsg)
	}

	close(ch)
}

// ── Windows native-binary resolution tests ──

// fakeStat returns a statFn that reports any path in `present` as existing
// and every other path as not-found. The returned os.FileInfo is a stub
// because resolveOpenCodeNativeFromShim only inspects the error.
func fakeStat(present ...string) func(string) (os.FileInfo, error) {
	set := make(map[string]struct{}, len(present))
	for _, p := range present {
		set[p] = struct{}{}
	}
	return func(path string) (os.FileInfo, error) {
		if _, ok := set[path]; ok {
			return nil, nil
		}
		return nil, errors.New("not found")
	}
}

func TestResolveOpenCodeNativeFromShimResolvesNpmShim(t *testing.T) {
	t.Parallel()

	// Reporter's exact layout from multica#1717.
	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")
	native := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")

	got := resolveOpenCodeNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeNativeFromShimReturnsEmptyWhenNativeMissing(t *testing.T) {
	t.Parallel()

	// Shim ends in .cmd but the bundled native binary isn't present (e.g.
	// platform package didn't install or layout changed). Caller must keep
	// the original shim path so PATH lookup still wins.
	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")

	got := resolveOpenCodeNativeFromShim(shim, fakeStat())
	if got != "" {
		t.Errorf("got %q, want empty (missing native binary)", got)
	}
}

func TestResolveOpenCodeNativeFromShimSkipsNonCmdPath(t *testing.T) {
	t.Parallel()

	// On macOS/Linux the path returned by exec.LookPath is the native
	// binary itself, with no .cmd extension. Helper should signal "no
	// rewrite needed" by returning empty.
	cases := []string{
		"/usr/local/bin/opencode",
		"C:\\nvm4w\\nodejs\\opencode.exe",
		"",
	}
	for _, p := range cases {
		if got := resolveOpenCodeNativeFromShim(p, fakeStat("anything")); got != "" {
			t.Errorf("path %q: got %q, want empty", p, got)
		}
	}
}

func TestResolveOpenCodeNativeFromShimAcceptsUppercaseExtension(t *testing.T) {
	t.Parallel()

	// Windows is case-insensitive on filesystem extensions. PATHEXT tokens
	// are commonly uppercase, and exec.LookPath can return either case.
	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.CMD")
	native := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe")

	got := resolveOpenCodeNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestResolveOpenCodeNativeFromShimFallsBackToBaseline(t *testing.T) {
	t.Parallel()

	// Older CPUs without AVX2 get `opencode-windows-x64-baseline` instead of
	// the default x64 build. Resolver should fall through and find it when
	// the primary x64 package isn't installed.
	shim := filepath.Join("C:\\nvm4w", "nodejs", "opencode.cmd")
	baseline := filepath.Join("C:\\nvm4w", "nodejs", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64-baseline", "bin", "opencode.exe")

	got := resolveOpenCodeNativeFromShim(shim, fakeStat(baseline))
	if got != baseline {
		t.Errorf("got %q, want %q", got, baseline)
	}
}

func TestOpencodeWindowsPackageCandidatesArm64(t *testing.T) {
	t.Parallel()

	// ARM64 hosts (Surface, Copilot+ PC) should try arm64 first so the
	// resolver doesn't accidentally pick up a leftover x64 install when
	// the matching arm64 package is present.
	got := opencodeWindowsPackageCandidates("arm64")
	want := []string{"opencode-windows-arm64", "opencode-windows-x64", "opencode-windows-x64-baseline"}
	if !equalStringSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOpencodeWindowsPackageCandidatesAmd64(t *testing.T) {
	t.Parallel()

	// amd64 (and any non-arm64) hosts try x64 → baseline → arm64. The arm64
	// fallback at the end covers the unusual case where only the arm64
	// package is installed; resolution still succeeds.
	got := opencodeWindowsPackageCandidates("amd64")
	want := []string{"opencode-windows-x64", "opencode-windows-x64-baseline", "opencode-windows-arm64"}
	if !equalStringSlice(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// fakeOpencodeScript returns a POSIX-sh script that impersonates `opencode`
// for argv / env capture. It writes the argv (one per line) to
// $OPENCODE_ARGS_FILE, the resolved PWD to $OPENCODE_PWD_FILE, and the
// permission config to $OPENCODE_PERMISSION_FILE. It emits a minimal completed
// step on stdout so the daemon's event loop terminates, then exits.
func fakeOpencodeScript() string {
	return `#!/bin/sh
if [ -n "$OPENCODE_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$OPENCODE_ARGS_FILE"
  done
fi
if [ -n "$OPENCODE_PWD_FILE" ]; then
  printf '%s\n' "$PWD" > "$OPENCODE_PWD_FILE"
fi
if [ -n "$OPENCODE_PERMISSION_FILE" ]; then
  printf '%s\n' "$OPENCODE_PERMISSION" > "$OPENCODE_PERMISSION_FILE"
fi
if [ -f "$PWD/opencode.json" ] && grep -Eq '"question"[[:space:]]*:[[:space:]]*"allow"' "$PWD/opencode.json"; then
  if [ "$OPENCODE_PERMISSION" = '{"*":"allow","question":"deny"}' ]; then
    printf '{"type":"error","timestamp":1,"sessionID":"ses_fake","error":{"name":"PermissionBypass","data":{"message":"question permission bypassed by env wildcard order"}}}\n'
    exit 0
  fi
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// TestOpencodeBackendAnchorsDirAndPWD pins the discovery-root fix from
// MUL-2416: OpenCode resolves its AGENTS.md walk-up and .opencode/skills
// project config scan from `--dir` and PWD. cmd.Dir alone is not enough
// because OpenCode reads PWD (inherited from the daemon) before falling
// back to process.cwd(). Without this anchor, skills written into the
// task workdir are silently invisible and the agent runs against the
// daemon's shell working directory.
func TestOpencodeBackendAnchorsDirAndPWD(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	pwdFile := filepath.Join(tempDir, "pwd.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	workDir := t.TempDir()

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
			"OPENCODE_PWD_FILE":  pwdFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:     workDir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	// argv should include `--dir <workDir>` immediately after the `run` /
	// `--format json` prefix and nowhere else.
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(args) < 2 || args[0] != "run" {
		t.Fatalf("expected first arg to be 'run', got %q", args)
	}
	dirIdx := -1
	for i, a := range args {
		if a == "--dir" {
			dirIdx = i
			break
		}
	}
	if dirIdx == -1 {
		t.Fatalf("expected --dir flag in argv, got %q", args)
	}
	if dirIdx+1 >= len(args) || args[dirIdx+1] != workDir {
		t.Fatalf("expected --dir %q, got args=%q", workDir, args)
	}

	// PWD inside the child process must resolve to the task workdir,
	// otherwise OpenCode's project discovery walk starts in the wrong
	// directory and silently misses .opencode/skills + AGENTS.md.
	gotPWD, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("read PWD file: %v", err)
	}
	if got := strings.TrimSpace(string(gotPWD)); got != workDir {
		t.Errorf("child PWD = %q, want %q", got, workDir)
	}
}

func TestOpencodeBackendInjectsThinkingVariant(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:         "opencode/deepseek-v4",
		ThinkingLevel: "max",
		CustomArgs:    []string{"--variant", "low", "--keep-me"},
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := splitNonEmptyLines(string(raw))
	if !containsAdjacent(args, "--model", "opencode/deepseek-v4") {
		t.Fatalf("expected --model opencode/deepseek-v4 in args: %v", args)
	}
	if !containsAdjacent(args, "--variant", "max") {
		t.Fatalf("expected daemon-injected --variant max in args: %v", args)
	}
	if argIndexOf(args, "--model") > argIndexOf(args, "--variant") {
		t.Errorf("expected --variant after --model for readability: %v", args)
	}
	count := 0
	for _, arg := range args {
		if arg == "--variant" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one --variant, got %d: %v", count, args)
	}
	if argIndexOf(args, "low") >= 0 {
		t.Errorf("filtered user --variant value still appears: %v", args)
	}
	if argIndexOf(args, "--keep-me") < 0 {
		t.Errorf("non-blocked custom arg was dropped: %v", args)
	}
}

func TestOpencodeBackendDoesNotUsePermissionEnvOverride(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	permissionFile := filepath.Join(tempDir, "permission.json")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_PERMISSION_FILE": permissionFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(permissionFile)
	if err != nil {
		t.Fatalf("read permission file: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "" {
		t.Fatalf("OPENCODE_PERMISSION = %q, want empty env override", got)
	}
}

func TestOpencodeBackendQuestionDenySurvivesUserConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	workDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workDir, "opencode.json"),
		[]byte(`{"permission":{"question":"allow"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write opencode config: %v", err)
	}

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:     workDir,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result status = %q, error = %q; want completed", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if !containsString(args, "--dangerously-skip-permissions") {
		t.Fatalf("expected daemon-mode argv to include --dangerously-skip-permissions, got %q", args)
	}
}

// TestOpencodeBackendBlocksDirOverride ensures user-supplied custom args
// cannot replace the daemon-managed `--dir` anchor. Letting custom args
// override it would re-introduce the MUL-2416 regression.
func TestOpencodeBackendBlocksDirOverride(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeScript()))

	workDir := t.TempDir()
	bogusDir := t.TempDir()

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			"OPENCODE_ARGS_FILE": argsFile,
		},
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Cwd:        workDir,
		Timeout:    5 * time.Second,
		CustomArgs: []string{"--dir", bogusDir},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, a := range args {
		if a == "--dir" {
			if i+1 >= len(args) || args[i+1] != workDir {
				t.Errorf("--dir was overridden by custom args: got %q", args)
			}
		}
		if a == bogusDir {
			t.Errorf("custom --dir value %q leaked into argv: %q", bogusDir, args)
		}
	}
}

// fakeOpencodeMidToolScript impersonates an `opencode run` whose provider
// stream dies mid-step: it prints a step_start and a terminal-error tool_use,
// then exits 0 on a clean EOF with no step_finish and no error event. This is
// the false-green shape from production — the recovered tool error does not
// rescue the run; the unclosed step is what fails it.
func fakeOpencodeMidToolScript() string {
	return `#!/bin/sh
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"tool_use","timestamp":2,"sessionID":"ses_fake","part":{"type":"tool","tool":"read","callID":"functions.read:1","state":{"status":"error","input":{"filePath":"/nope.md"},"error":"File not found"}}}\n'
exit 0
`
}

// TestOpencodeBackendFailsOnStreamEndingMidTool proves the terminal-signal
// guard over the full clean-EOF/exit-0 path, not just the scanner loop: a run
// whose stream ends with an open step must surface as Result.Status == "failed"
// even though opencode exited 0 with no error event and its last tool merely
// reported a recovered error.
func TestOpencodeBackendFailsOnStreamEndingMidTool(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeMidToolScript()))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("result status = %q, error = %q; want failed", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "terminal signal") {
		t.Errorf("result error = %q, want it to mention the missing terminal signal", result.Error)
	}
}

// fakeOpencodeStepThenExit1Script impersonates an `opencode run` that crashes
// mid-step: it opens a step, then the process itself dies nonzero with no
// step_finish and no error event.
func fakeOpencodeStepThenExit1Script() string {
	return `#!/bin/sh
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
exit 1
`
}

// TestOpencodeBackendAppendsExitDetailOnMidStepCrash pins the second fix: when
// the terminal-signal guard has already failed the run AND the process exited
// nonzero, the exit detail is appended so the crash signal / exit code is not
// lost — the errMsg carries both the missing-terminal-signal reason and the
// process exit status.
func TestOpencodeBackendAppendsExitDetailOnMidStepCrash(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakePath := filepath.Join(tempDir, "opencode")
	writeTestExecutable(t, fakePath, []byte(fakeOpencodeStepThenExit1Script()))

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("result status = %q, error = %q; want failed", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "terminal signal") {
		t.Errorf("result error = %q, want it to mention the missing terminal signal", result.Error)
	}
	if !strings.Contains(result.Error, "exit status 1") {
		t.Errorf("result error = %q, want it to include the process exit status", result.Error)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
