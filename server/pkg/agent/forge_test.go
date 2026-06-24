package agent

import (
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

func TestParseForgeConversationUsage(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		model string
		want  *TokenUsage
	}{
		{
			name:  "prompt/completion tokens",
			json:  `{"usage":{"prompt_tokens":100,"completion_tokens":50}}`,
			model: "anthropic/claude-sonnet-4-20250514",
			want:  &TokenUsage{InputTokens: 100, OutputTokens: 50},
		},
		{
			name:  "input/output aliases",
			json:  `{"usage":{"input":200,"output":80}}`,
			model: "openai/gpt-4o",
			want:  &TokenUsage{InputTokens: 200, OutputTokens: 80},
		},
		{
			name:  "nested under conversation",
			json:  `{"conversation":{"usage":{"prompt_tokens":10,"completion_tokens":5}}}`,
			model: "m",
			want:  &TokenUsage{InputTokens: 10, OutputTokens: 5},
		},
		{
			name:  "no usage key",
			json:  `{"messages":[]}`,
			model: "m",
			want:  nil,
		},
		{
			name:  "empty usage object",
			json:  `{"usage":{}}`,
			model: "m",
			want:  nil,
		},
		{
			name:  "total tokens only ignored without in/out",
			json:  `{"usage":{"total_tokens":42}}`,
			model: "m",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseForgeConversationUsage([]byte(tc.json), tc.model)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil usage, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected usage %+v, got nil", tc.want)
			}
			// usage is keyed by model name; extract the single entry.
			var u TokenUsage
			for _, v := range got {
				u = v
			}
			if u.InputTokens != tc.want.InputTokens || u.OutputTokens != tc.want.OutputTokens {
				t.Errorf("usage = %+v, want %+v", u, tc.want)
			}
		})
	}
}

func TestForgeProcessStream(t *testing.T) {
	// ForgeCode emits rendered Markdown with ANSI styling on stdout.
	// processStream strips ANSI and forwards non-empty lines as text messages.
	input := "\x1b[1mStarting task\x1b[0m\n\n\x1b[32mDone\x1b[0m\n"
	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}

	result := b.processStream(strings.NewReader(input), ch)
	close(ch)

	var texts []string
	for msg := range ch {
		if msg.Type == MessageText {
			texts = append(texts, msg.Content)
		}
	}
	wantTexts := []string{"Starting task", "Done"}
	if len(texts) != len(wantTexts) {
		t.Fatalf("expected %d text messages, got %d: %v", len(wantTexts), len(texts), texts)
	}
	for i, want := range wantTexts {
		if texts[i] != want {
			t.Errorf("texts[%d] = %q, want %q", i, texts[i], want)
		}
	}
	if result.status != "completed" {
		t.Errorf("status = %q, want completed", result.status)
	}
	if !strings.Contains(result.output, "Starting task") || !strings.Contains(result.output, "Done") {
		t.Errorf("output missing expected text: %q", result.output)
	}
}

func TestForgeProcessStreamStripsPureAnsiLines(t *testing.T) {
	// A line that is only ANSI escape codes should not produce an empty
	// text message after stripping.
	input := "\x1b[2J\x1b[H\nvisible\n"
	ch := make(chan Message, 16)
	b := &forgeBackend{cfg: Config{Logger: slog.Default()}}

	b.processStream(strings.NewReader(input), ch)
	close(ch)

	var texts []string
	for msg := range ch {
		if msg.Type == MessageText {
			texts = append(texts, msg.Content)
		}
	}
	if len(texts) != 1 || texts[0] != "visible" {
		t.Fatalf("expected single 'visible' message, got %v", texts)
	}
}
