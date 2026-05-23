package agent

import (
	"encoding/json"
	"log/slog"
	"testing"
)

func TestBuildWarpArgsIncludesCoreOptions(t *testing.T) {
	t.Parallel()

	args := buildWarpArgs("fix lint", ExecOptions{
		Cwd:             "/tmp/repo",
		Model:           "auto",
		ResumeSessionID: "conv-123",
		SystemPrompt:    "You are in test mode.",
		McpConfig:       json.RawMessage(`{"github":{"url":"https://example.com/mcp"}}`),
	}, slog.Default())

	assertHasArg(t, args, "agent")
	assertHasArg(t, args, "run")
	assertHasArg(t, args, "--output-format")
	assertHasArg(t, args, "json")
	assertHasArg(t, args, "--cwd")
	assertHasArg(t, args, "/tmp/repo")
	assertHasArg(t, args, "--model")
	assertHasArg(t, args, "auto")
	assertHasArg(t, args, "--conversation")
	assertHasArg(t, args, "conv-123")
	assertHasArg(t, args, "--mcp")
	assertHasArg(t, args, `{"github":{"url":"https://example.com/mcp"}}`)
	assertHasArg(t, args, "You are in test mode.\n\n---\n\nfix lint")
}

func TestBuildWarpArgsFiltersBlockedCustomFlags(t *testing.T) {
	t.Parallel()

	args := buildWarpArgs("task", ExecOptions{
		CustomArgs: []string{
			"--output-format", "json",
			"--cwd", "/etc",
			"--model", "bad",
			"--conversation", "bad-conv",
			"--prompt", "overwrite",
			"--debug",
		},
	}, slog.Default())

	assertHasArg(t, args, "--debug")
	assertNoArg(t, args, "/etc")
	assertNoArg(t, args, "bad-conv")
	assertNoArg(t, args, "overwrite")
}

func TestWarpEventErrorText(t *testing.T) {
	t.Parallel()

	if got := (warpEvent{Error: "boom"}).errorText(); got != "boom" {
		t.Fatalf("errorText() = %q, want boom", got)
	}
	if got := (warpEvent{Type: "system", EventType: "error", Text: "failed"}).errorText(); got != "failed" {
		t.Fatalf("errorText() = %q, want failed", got)
	}
	if got := (warpEvent{Type: "system", EventType: "run_started"}).errorText(); got != "" {
		t.Fatalf("errorText() = %q, want empty", got)
	}
}

func assertHasArg(t *testing.T, args []string, want string) {
	t.Helper()
	if !containsArg(args, want) {
		t.Fatalf("args %q missing %q", args, want)
	}
}

func assertNoArg(t *testing.T, args []string, bad string) {
	t.Helper()
	if containsArg(args, bad) {
		t.Fatalf("args %q unexpectedly contained %q", args, bad)
	}
}

func containsArg(args []string, needle string) bool {
	for _, part := range args {
		if part == needle {
			return true
		}
	}
	return false
}
