package agent

import (
	"strings"
	"testing"
)

func TestEstimateInputTokens(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		system string
		want   int64
	}{
		{name: "empty", prompt: "", system: "", want: 0},
		{name: "exact multiple", prompt: "abcd", system: "", want: 1},
		{name: "rounds up", prompt: "abcde", system: "", want: 2},
		{name: "includes system prompt", prompt: "abcd", system: "efgh", want: 2},
		{name: "rule of thumb 4 bytes per token", prompt: strings.Repeat("x", 400), system: "", want: 100},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateInputTokens(tt.prompt, tt.system); got != tt.want {
				t.Fatalf("EstimateInputTokens(%q, %q) = %d, want %d", tt.prompt, tt.system, got, tt.want)
			}
		})
	}
}

func TestBuildDryRunResult(t *testing.T) {
	res := BuildDryRunResult(
		"hello world hello world",
		ExecOptions{SystemPrompt: "be helpful", MaxTurns: 25},
		"claude",
	)
	if res.Status != "completed" {
		t.Errorf("status = %q, want %q", res.Status, "completed")
	}
	if !strings.Contains(res.Output, "[dry-run]") {
		t.Errorf("Output missing dry-run marker: %q", res.Output)
	}
	if !strings.Contains(res.Output, "claude (stream-json)") {
		t.Errorf("Output missing claude launch header: %q", res.Output)
	}
	if !strings.Contains(res.Output, "max_turns: 25") {
		t.Errorf("Output missing max_turns: %q", res.Output)
	}
	usage, ok := res.Usage["_dry_run"]
	if !ok {
		t.Fatalf("Usage missing _dry_run key: %+v", res.Usage)
	}
	// 23 prompt + 10 system = 33 bytes, /4 round-up = 9
	if usage.InputTokens != 9 {
		t.Errorf("dry-run input tokens = %d, want 9", usage.InputTokens)
	}
	if usage.OutputTokens != 0 {
		t.Errorf("dry-run must not record output tokens, got %d", usage.OutputTokens)
	}
}

func TestBuildDryRunResultUnknownProviderFallsBack(t *testing.T) {
	res := BuildDryRunResult("hi", ExecOptions{}, "made-up-provider")
	if !strings.Contains(res.Output, "made-up-provider") {
		t.Fatalf("Output should include unknown provider name verbatim: %q", res.Output)
	}
}
