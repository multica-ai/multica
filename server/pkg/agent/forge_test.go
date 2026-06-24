package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSplitForgeModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		provider string
		model    string
		ok       bool
	}{
		{name: "standard", input: "anthropic/claude-sonnet-4-20250514", provider: "anthropic", model: "claude-sonnet-4-20250514", ok: true},
		{name: "openai", input: "openai/gpt-4o", provider: "openai", model: "gpt-4o", ok: true},
		{name: "openrouter", input: "openrouter/anthropic/claude-3.7-sonnet", provider: "openrouter", model: "anthropic/claude-3.7-sonnet", ok: true},
		{name: "no slash", input: "claude-sonnet", ok: false},
		{name: "leading slash", input: "/model", ok: false},
		{name: "trailing slash", input: "anthropic/", ok: false},
		{name: "empty", input: "", ok: false},
		{name: "spaces trimmed", input: " anthropic / claude-sonnet ", provider: "anthropic", model: "claude-sonnet", ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, m, ok := splitForgeModel(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if p != tc.provider {
				t.Errorf("provider = %q, want %q", p, tc.provider)
			}
			if m != tc.model {
				t.Errorf("model = %q, want %q", m, tc.model)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "hello world", want: "hello world"},
		{name: "sgr color reset", input: "\x1b[31mred text\x1b[0m", want: "red text"},
		{name: "bold", input: "\x1b[1mbold\x1b[22m", want: "bold"},
		{name: "multiple segments", input: "\x1b[32mok\x1b[0m \x1b[1mdone\x1b[0m", want: "ok done"},
		{name: "osc title bel", input: "\x1b]0;title\x07text", want: "text"},
		{name: "osc title st", input: "\x1b]2;win\x1b\\body", want: "body"},
		{name: "empty", input: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripANSI(tc.input)
			if got != tc.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestForgeProcessStream verifies that processStream no longer forwards stdout
// lines as text messages (the old behaviour leaked tool output into the
// agent's prose). Instead it buffers lines for the fallback path and forwards
// a status heartbeat per non-empty line so the daemon's idle watchdog sees
// activity. The first heartbeat carries the conversation id so the daemon
// can pin the resume pointer.
func TestForgeProcessStream(t *testing.T) {
	input := "\x1b[1mStarting task\x1b[0m\n\n\x1b[32mDone\x1b[0m\n"
	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}

	result := b.processStream(strings.NewReader(input), ch, "conv-123")
	close(ch)

	var statuses []Message
	var texts []string
	for msg := range ch {
		switch msg.Type {
		case MessageStatus:
			statuses = append(statuses, msg)
		case MessageText:
			texts = append(texts, msg.Content)
		}
	}

	// No text should be emitted during the live stream — tool output would
	// otherwise leak into the agent's prose. Text is produced only from the
	// post-run conversation dump (replayFromDump), or as a fallback when the
	// dump is unavailable.
	if len(texts) != 0 {
		t.Fatalf("expected no text messages during live stream, got %d: %v", len(texts), texts)
	}

	// Each non-empty line forwards one status heartbeat; the first carries
	// the conversation id.
	if len(statuses) != 2 {
		t.Fatalf("expected 2 status heartbeats (one per non-empty line), got %d", len(statuses))
	}
	if statuses[0].SessionID != "conv-123" {
		t.Errorf("first heartbeat SessionID = %q, want conv-123", statuses[0].SessionID)
	}
	if statuses[1].SessionID != "" {
		t.Errorf("subsequent heartbeat SessionID = %q, want empty", statuses[1].SessionID)
	}

	if result.status != "completed" {
		t.Errorf("status = %q, want completed", result.status)
	}
	// Buffered output preserves the ANSI-stripped lines for the fallback path.
	if !strings.Contains(result.output, "Starting task") || !strings.Contains(result.output, "Done") {
		t.Errorf("buffered output missing expected text: %q", result.output)
	}
}

func TestForgeProcessStreamStripsPureAnsiLines(t *testing.T) {
	// A line that is only ANSI escape codes should not produce a heartbeat
	// (it is empty after stripping).
	input := "\x1b[2J\x1b[H\nvisible\n"
	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}

	b.processStream(strings.NewReader(input), ch, "")
	close(ch)

	var count int
	for msg := range ch {
		if msg.Type == MessageStatus {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 heartbeat (pure-ANSI line skipped), got %d", count)
	}
}

// TestReplayFromDump verifies that the dump replay classifies messages the
// same way the OpenCode backend does: assistant reasoning → thinking, tool
// calls → tool-use, assistant prose → text, tool results → tool-result.
// System and user turns are skipped.
func TestReplayFromDump(t *testing.T) {
	msgs := []forgeDumpMessage{
		// System turn — skipped.
		{Text: &forgeDumpText{Role: "System", Content: "system prompt"}},
		// User turn — skipped.
		{Text: &forgeDumpText{Role: "User", Content: "user prompt"}},
		// Assistant turn with reasoning + a tool call + no prose.
		{
			Text: &forgeDumpText{
				Role: "Assistant",
				ReasoningDetails: []forgeDumpReasoning{
					{Text: "I should read the file first."},
				},
				ToolCalls: []forgeDumpToolCall{
					{Name: "read", CallID: "call_1", Arguments: map[string]any{"file_path": "/tmp/a.txt"}},
				},
			},
		},
		// Tool result for call_1.
		{
			Tool: &forgeDumpTool{
				Name:   "read",
				CallID: "call_1",
				Output: forgeDumpOutput{
					Values: []forgeDumpToolValue{{Text: "file contents"}},
				},
			},
		},
		// Assistant turn with prose (final answer).
		{
			Text: &forgeDumpText{
				Role:    "Assistant",
				Content: "The file contains: file contents",
			},
		},
	}

	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}
	output := b.replayFromDump(msgs, ch)
	close(ch)

	var got []Message
	for msg := range ch {
		got = append(got, msg)
	}

	// Expected sequence: thinking, tool-use, tool-result, text.
	wantTypes := []MessageType{MessageThinking, MessageToolUse, MessageToolResult, MessageText}
	if len(got) != len(wantTypes) {
		t.Fatalf("expected %d messages, got %d: %+v", len(wantTypes), len(got), got)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Errorf("msg[%d].Type = %v, want %v", i, got[i].Type, want)
		}
	}

	// Tool-use carries name + call id + input.
	if got[1].Tool != "read" || got[1].CallID != "call_1" {
		t.Errorf("tool-use = %+v, want read/call_1", got[1])
	}
	if got[1].Input["file_path"] != "/tmp/a.txt" {
		t.Errorf("tool-use input = %+v, want file_path=/tmp/a.txt", got[1].Input)
	}
	// Tool-result carries name + call id + output.
	if got[2].Tool != "read" || got[2].CallID != "call_1" || got[2].Output != "file contents" {
		t.Errorf("tool-result = %+v, want read/call_1/file contents", got[2])
	}
	// Text carries the assistant prose and output accumulates it.
	if got[3].Content != "The file contains: file contents" {
		t.Errorf("text content = %q, want the assistant prose", got[3].Content)
	}
	if output != "The file contains: file contents" {
		t.Errorf("accumulated output = %q, want the assistant prose", output)
	}
}

// TestReplayFromDumpSkipsEmptyContent verifies that an assistant turn whose
// content is empty (a pure tool-call turn) does not emit an empty text
// message.
func TestReplayFromDumpSkipsEmptyContent(t *testing.T) {
	msgs := []forgeDumpMessage{
		{Text: &forgeDumpText{
			Role:    "Assistant",
			Content: "   ",
			ToolCalls: []forgeDumpToolCall{
				{Name: "shell", CallID: "c", Arguments: map[string]any{"cmd": "ls"}},
			},
		}},
	}
	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}
	b.replayFromDump(msgs, ch)
	close(ch)

	for msg := range ch {
		if msg.Type == MessageText {
			t.Fatalf("did not expect a text message for whitespace-only content, got %q", msg.Content)
		}
	}
}

// TestForgeUsageFromMessages verifies that cumulative counters are reduced to
// the latest non-zero entry, and that cached tokens are captured.
func TestForgeUsageFromMessages(t *testing.T) {
	msgs := []forgeDumpMessage{
		// First assistant turn: 100 in / 10 out / 0 cached.
		{
			Text: &forgeDumpText{Role: "Assistant", Content: "first"},
			Usage: &forgeDumpUsage{
				PromptTokens:     forgeDumpTokenCount{Actual: 100},
				CompletionTokens: forgeDumpTokenCount{Actual: 10},
				CachedTokens:     forgeDumpTokenCount{Actual: 0},
			},
		},
		// Second assistant turn (cumulative): 200 in / 25 out / 150 cached.
		{
			Text: &forgeDumpText{Role: "Assistant", Content: "second"},
			Usage: &forgeDumpUsage{
				PromptTokens:     forgeDumpTokenCount{Actual: 200},
				CompletionTokens: forgeDumpTokenCount{Actual: 25},
				CachedTokens:     forgeDumpTokenCount{Actual: 150},
			},
		},
	}

	u := forgeUsageFromMessages(msgs)
	if u == nil {
		t.Fatal("expected usage, got nil")
	}
	if u.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200 (latest cumulative)", u.InputTokens)
	}
	if u.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", u.OutputTokens)
	}
	if u.CacheReadTokens != 150 {
		t.Errorf("CacheReadTokens = %d, want 150", u.CacheReadTokens)
	}
}

func TestForgeUsageFromMessagesNone(t *testing.T) {
	// No usage objects at all.
	if u := forgeUsageFromMessages(nil); u != nil {
		t.Fatalf("expected nil, got %+v", u)
	}
	// All-zero usage entries are ignored.
	msgs := []forgeDumpMessage{
		{Text: &forgeDumpText{Role: "Assistant"}, Usage: &forgeDumpUsage{}},
	}
	if u := forgeUsageFromMessages(msgs); u != nil {
		t.Fatalf("expected nil for all-zero usage, got %+v", u)
	}
}

// TestForgeConversationDumpParse round-trips a realistic dump document
// through the typed unmarshaler to confirm the struct tags match the
// ForgeCode schema captured from a real run.
func TestForgeConversationDumpParse(t *testing.T) {
	raw := `{
	  "conversation": {
	    "id": "conv-1",
	    "context": {
	      "messages": [
	        {
	          "text": {
	            "role": "Assistant",
	            "content": "",
	            "tool_calls": [
	              {"name": "read", "call_id": "call_abc", "arguments": {"file_path": "/tmp/x"}}
	            ],
	            "reasoning_details": [{"text": "thinking here"}]
	          },
	          "usage": {
	            "prompt_tokens": {"actual": 500},
	            "completion_tokens": {"actual": 20},
	            "cached_tokens": {"actual": 400}
	          }
	        },
	        {
	          "tool": {
	            "name": "read",
	            "call_id": "call_abc",
	            "output": {
	              "is_error": false,
	              "values": [{"text": "hello"}]
	            }
	          }
	        }
	      ]
	    }
	  }
	}`

	var doc forgeConversationDump
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if doc.Conversation.ID != "conv-1" {
		t.Errorf("conversation id = %q, want conv-1", doc.Conversation.ID)
	}
	msgs := doc.Conversation.Context.Messages
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Text == nil || msgs[0].Text.Role != "Assistant" {
		t.Fatalf("msg[0] text missing or wrong role: %+v", msgs[0].Text)
	}
	if len(msgs[0].Text.ToolCalls) != 1 || msgs[0].Text.ToolCalls[0].Name != "read" {
		t.Errorf("msg[0] tool_calls mismatch: %+v", msgs[0].Text.ToolCalls)
	}
	if msgs[0].Usage == nil || msgs[0].Usage.PromptTokens.Actual != 500 {
		t.Errorf("msg[0] usage mismatch: %+v", msgs[0].Usage)
	}
	if msgs[1].Tool == nil || msgs[1].Tool.Name != "read" {
		t.Fatalf("msg[1] tool missing: %+v", msgs[1].Tool)
	}
	if msgs[1].Tool.Output.Values[0].Text != "hello" {
		t.Errorf("msg[1] output text = %q, want hello", msgs[1].Tool.Output.Values[0].Text)
	}
}
