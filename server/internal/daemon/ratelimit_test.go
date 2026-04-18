package daemon

import (
	"fmt"
	"testing"
)

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		result TaskResult
		want   bool
	}{
		{"nil error, empty result", nil, TaskResult{}, false},
		{"rate limit in error", fmt.Errorf("rate limit exceeded"), TaskResult{}, true},
		{"hit your limit in error", fmt.Errorf("You've hit your limit · resets 5am"), TaskResult{}, true},
		{"429 in error", fmt.Errorf("HTTP 429 Too Many Requests"), TaskResult{}, true},
		{"overloaded in error", fmt.Errorf("API is overloaded"), TaskResult{}, true},
		{"rate limit in comment", nil, TaskResult{Comment: "rate limit exceeded"}, true},
		{"hit your limit in comment", nil, TaskResult{Comment: "You've hit your limit · resets 5am (Europe/Berlin)"}, true},
		{"normal error", fmt.Errorf("connection refused"), TaskResult{}, false},
		{"normal comment", nil, TaskResult{Comment: "task completed successfully"}, false},
		{"RateLimitError name", fmt.Errorf("RateLimitError"), TaskResult{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRateLimitError(tt.err, tt.result)
			if got != tt.want {
				t.Errorf("isRateLimitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickFallbackProvider(t *testing.T) {
	d := &Daemon{cfg: Config{
		Agents: map[string]AgentEntry{
			"claude": {Path: "/usr/bin/claude"},
			"codex":  {Path: "/usr/bin/codex"},
		},
	}}

	// claude → codex (first available)
	if got := d.pickFallbackProvider("claude"); got != "codex" {
		t.Errorf("pickFallbackProvider(claude) = %q, want %q", got, "codex")
	}

	// codex → claude (first available)
	if got := d.pickFallbackProvider("codex"); got != "claude" {
		t.Errorf("pickFallbackProvider(codex) = %q, want %q", got, "claude")
	}

	// unknown provider → ""
	if got := d.pickFallbackProvider("unknown"); got != "" {
		t.Errorf("pickFallbackProvider(unknown) = %q, want %q", got, "")
	}

	// No fallback available
	d2 := &Daemon{cfg: Config{
		Agents: map[string]AgentEntry{
			"claude": {Path: "/usr/bin/claude"},
		},
	}}
	if got := d2.pickFallbackProvider("codex"); got != "claude" {
		t.Errorf("pickFallbackProvider(codex) with only claude = %q, want %q", got, "claude")
	}
}
