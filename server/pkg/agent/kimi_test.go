package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestKimiProcessAssistantText(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	line := `{"role":"assistant","content":[{"type":"text","text":"Hello, world!"}]}`

	result := b.processEvents(strings.NewReader(line), ch)

	if result.status != "completed" {
		t.Fatalf("expected completed, got %q", result.status)
	}
	if result.output != "Hello, world!" {
		t.Fatalf("expected output 'Hello, world!', got %q", result.output)
	}

	// Should have 2 messages: status "running" + text
	var msgs []Message
	for len(msgs) < 2 {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			t.Fatal("expected 2 messages, got", len(msgs))
		}
	}
	if msgs[0].Type != MessageStatus || msgs[0].Status != "running" {
		t.Fatalf("expected first message to be status running, got %+v", msgs[0])
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "Hello, world!" {
		t.Fatalf("expected second message to be text, got %+v", msgs[1])
	}
}

func TestKimiProcessAssistantThinking(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	line := `{"role":"assistant","content":[{"type":"think","think":"I need to check the files"},{"type":"text","text":"Let me look at the files."}]}`

	result := b.processEvents(strings.NewReader(line), ch)

	if result.output != "Let me look at the files." {
		t.Fatalf("expected text output, got %q", result.output)
	}

	var msgs []Message
	for len(msgs) < 3 {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			t.Fatal("expected 3 messages")
		}
	}

	// status, thinking, text
	if msgs[1].Type != MessageThinking || msgs[1].Content != "I need to check the files" {
		t.Fatalf("expected thinking message, got %+v", msgs[1])
	}
	if msgs[2].Type != MessageText || msgs[2].Content != "Let me look at the files." {
		t.Fatalf("expected text message, got %+v", msgs[2])
	}
}

func TestKimiProcessToolCalls(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	line := `{"role":"assistant","content":[{"type":"think","think":"Running ls"},{"type":"text","text":"Let me list the files."}],"tool_calls":[{"type":"function","id":"tool_abc123","function":{"name":"Shell","arguments":"{\"command\": \"ls -la\"}"}}]}`

	result := b.processEvents(strings.NewReader(line), ch)

	if result.output != "Let me list the files." {
		t.Fatalf("expected text output, got %q", result.output)
	}

	// Drain all messages
	var msgs []Message
drain:
	for {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			break drain
		}
	}

	// Find the tool_use message
	var toolMsg *Message
	for i := range msgs {
		if msgs[i].Type == MessageToolUse {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected a tool_use message")
	}
	if toolMsg.Tool != "Shell" {
		t.Fatalf("expected tool 'Shell', got %q", toolMsg.Tool)
	}
	if toolMsg.CallID != "tool_abc123" {
		t.Fatalf("expected callID 'tool_abc123', got %q", toolMsg.CallID)
	}
	if toolMsg.Input["command"] != "ls -la" {
		t.Fatalf("expected input command 'ls -la', got %v", toolMsg.Input["command"])
	}
}

func TestKimiProcessToolResult(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	line := `{"role":"tool","content":[{"type":"text","text":"<system>Command executed successfully.</system>"},{"type":"text","text":"total 42\ndrwxr-xr-x 5 user staff 160 Apr 21 ."}],"tool_call_id":"tool_abc123"}`

	b.processEvents(strings.NewReader(line), ch)

	var msgs []Message
drain:
	for {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			break drain
		}
	}

	var resultMsg *Message
	for i := range msgs {
		if msgs[i].Type == MessageToolResult {
			resultMsg = &msgs[i]
			break
		}
	}
	if resultMsg == nil {
		t.Fatal("expected a tool_result message")
	}
	if resultMsg.CallID != "tool_abc123" {
		t.Fatalf("expected callID 'tool_abc123', got %q", resultMsg.CallID)
	}
	if !strings.Contains(resultMsg.Output, "total 42") {
		t.Fatalf("expected output to contain 'total 42', got %q", resultMsg.Output)
	}
}

func TestKimiProcessMultipleToolResults(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	// Multiple content blocks in tool result should be joined with newline
	line := `{"role":"tool","content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}],"tool_call_id":"tool_xyz"}`

	b.processEvents(strings.NewReader(line), ch)

	var msgs []Message
drain:
	for {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			break drain
		}
	}

	var resultMsg *Message
	for i := range msgs {
		if msgs[i].Type == MessageToolResult {
			resultMsg = &msgs[i]
			break
		}
	}
	if resultMsg == nil {
		t.Fatal("expected a tool_result message")
	}
	if resultMsg.Output != "line1\nline2" {
		t.Fatalf("expected joined output 'line1\\nline2', got %q", resultMsg.Output)
	}
}

func TestKimiProcessFullTurn(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	// Simulate a full turn: assistant with tool call, then tool result, then assistant text
	input := `{"role":"assistant","content":[{"type":"think","think":"I should check the files"},{"type":"text","text":"Let me list the files."}],"tool_calls":[{"type":"function","id":"tool_1","function":{"name":"Shell","arguments":"{\"command\": \"ls\"}"}}]}
{"role":"tool","content":[{"type":"text","text":"file1.txt\nfile2.txt"}],"tool_call_id":"tool_1"}
{"role":"assistant","content":[{"type":"text","text":"I found 2 files: file1.txt and file2.txt"}]}`

	result := b.processEvents(strings.NewReader(input), ch)

	if result.status != "completed" {
		t.Fatalf("expected completed, got %q", result.status)
	}
	// Output should be the concatenated text from assistant messages
	expected := "Let me list the files.I found 2 files: file1.txt and file2.txt"
	if result.output != expected {
		t.Fatalf("expected output %q, got %q", expected, result.output)
	}
}

func TestKimiProcessEmptyInput(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	result := b.processEvents(strings.NewReader(""), ch)

	if result.status != "completed" {
		t.Fatalf("expected completed, got %q", result.status)
	}
	if result.output != "" {
		t.Fatalf("expected empty output, got %q", result.output)
	}
}

func TestKimiProcessInvalidJSON(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	// Invalid JSON lines should be skipped
	input := "not json\nalso not json\n"

	result := b.processEvents(strings.NewReader(input), ch)

	if result.status != "completed" {
		t.Fatalf("expected completed, got %q", result.status)
	}
	if result.output != "" {
		t.Fatalf("expected empty output, got %q", result.output)
	}
}

func TestKimiProcessEmptyLines(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	input := "\n\n{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"hi\"}]}\n\n"

	result := b.processEvents(strings.NewReader(input), ch)

	if result.output != "hi" {
		t.Fatalf("expected 'hi', got %q", result.output)
	}
}

func TestKimiBlockedArgs(t *testing.T) {
	t.Parallel()

	expectedBlocked := []string{"--print", "--output-format", "--input-format", "-y", "--yolo", "--yes", "-p", "--prompt", "-S", "--session", "-r"}
	for _, arg := range expectedBlocked {
		if _, ok := kimiBlockedArgs[arg]; !ok {
			t.Errorf("expected %q to be in kimiBlockedArgs", arg)
		}
	}
}

func TestKimiBuildArgs(t *testing.T) {
	t.Parallel()

	args := buildKimiArgs("test-session", ExecOptions{Model: "kimi-for-coding"}, slog.Default())

	// Check essential flags are present
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--print") {
		t.Error("expected --print in args")
	}
	if !strings.Contains(argStr, "--output-format stream-json") {
		t.Error("expected --output-format stream-json in args")
	}
	if !strings.Contains(argStr, "-y") {
		t.Error("expected -y in args")
	}
	if !strings.Contains(argStr, "-S test-session") {
		t.Error("expected -S test-session in args")
	}
	if !strings.Contains(argStr, "-m kimi-for-coding") {
		t.Error("expected -m kimi-for-coding in args")
	}
}

func TestKimiBuildArgsWithResumeSession(t *testing.T) {
	t.Parallel()

	args := buildKimiArgs("prior-session-id", ExecOptions{ResumeSessionID: "prior-session-id"}, slog.Default())

	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "-S prior-session-id") {
		t.Error("expected -S prior-session-id in args")
	}
}

func TestKimiBuildArgsFiltersBlocked(t *testing.T) {
	t.Parallel()

	// User tries to override blocked flags via custom_args — should be filtered.
	args := buildKimiArgs("s1", ExecOptions{
		CustomArgs: []string{"--output-format", "text", "--yolo"},
	}, slog.Default())

	for _, arg := range args {
		if arg == "--yolo" {
			t.Error("--yolo should have been filtered from custom_args")
		}
		if arg == "text" {
			// Could be a legitimate value if not preceded by --output-format
			// But --output-format should have been filtered, so "text" shouldn't
			// appear as its value either.
		}
	}
}

func TestKimiProcessSystemRole(t *testing.T) {
	t.Parallel()

	b := &kimiBackend{cfg: Config{}}
	ch := make(chan Message, 256)

	// System messages should be silently skipped
	line := `{"role":"system","content":[{"type":"text","text":"system info"}]}`

	result := b.processEvents(strings.NewReader(line), ch)

	if result.status != "completed" {
		t.Fatalf("expected completed, got %q", result.status)
	}
	if result.output != "" {
		t.Fatalf("expected empty output (system messages ignored), got %q", result.output)
	}
}
