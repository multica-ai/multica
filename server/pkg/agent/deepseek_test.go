package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsDeepseekBackend(t *testing.T) {
	t.Parallel()
	b, err := New("DeepSeek-TUI", Config{ExecutablePath: "/nonexistent/deepseek-tui"})
	if err != nil {
		t.Fatalf("New(deepseek) error: %v", err)
	}
	if _, ok := b.(*deepseekBackend); !ok {
		t.Fatalf("expected *deepseekBackend, got %T", b)
	}
}

func TestDeepseekToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"read_file", "read_file"},
		{"read", "read_file"},
		{"write_file", "write_file"},
		{"write", "write_file"},
		{"edit_file", "edit_file"},
		{"edit", "edit_file"},
		{"patch", "edit_file"},
		{"shell", "terminal"},
		{"bash", "terminal"},
		{"terminal", "terminal"},
		{"run_command", "terminal"},
		{"search", "search_files"},
		{"search_files", "search_files"},
		{"grep", "search_files"},
		{"glob", "glob"},
		{"web_search", "web_search"},
		{"web_fetch", "web_fetch"},
		{"fetch", "web_fetch"},
		{"todo_write", "todo_write"},
		{"custom_thing", "custom_thing"},
		{"", ""},
	}
	for _, tt := range tests {
		got := deepseekToolName(tt.name)
		if got != tt.want {
			t.Errorf("deepseekToolName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func fakeDeepseekExecScript(mode string) string {
	switch mode {
	case "success":
		return `#!/bin/sh
if [ -n "$DEEPSEEK_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEEPSEEK_ARGS_FILE"
  done
fi
printf '%s\n' '{"mode":"agent","model":"deepseek-v4-pro","output":"Hello from DeepSeek!","tools":[{"name":"write_file","success":true,"output":"created hello.txt"}],"status":"completed","error":null,"usage":{"input_tokens":25,"output_tokens":7,"cached_input_tokens":3}}'
`
	case "oneshot_success":
		return `#!/bin/sh
printf '%s\n' '{"mode":"one-shot","model":"deepseek-v4-pro","success":true,"output":"Hi there!"}'
`
	case "failure":
		return `#!/bin/sh
printf '%s\n' '{"mode":"agent","model":"deepseek-v4-pro","output":"","tools":[],"status":"failed","error":"no API key"}'
exit 1
`
	default:
		return "#!/bin/sh\nexit 1\n"
	}
}

func TestDeepseekBackendExecJSONProtocol(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek-tui")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekExecScript("success")), 0o755); err != nil {
		t.Fatalf("write fake deepseek-tui: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "say hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "Hello from DeepSeek!" {
			t.Errorf("output = %q, want Hello from DeepSeek!", result.Output)
		}
		usage := result.Usage["deepseek-v4-pro"]
		if usage.InputTokens != 25 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
			t.Errorf("usage = %+v, want input=25 output=7 cache_read=3", usage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
	<-done

	if len(messages) < 3 {
		t.Fatalf("messages = %+v, want tool use/result and final text", messages)
	}
	if messages[0].Type != MessageToolUse || messages[0].Tool != "write_file" {
		t.Fatalf("first message = %+v, want write_file tool use", messages[0])
	}
	if messages[len(messages)-1].Type != MessageText || messages[len(messages)-1].Content != "Hello from DeepSeek!" {
		t.Fatalf("last message = %+v, want final text", messages[len(messages)-1])
	}
}

func TestDeepseekBackendOneShotSuccessJSON(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek-tui")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekExecScript("oneshot_success")), 0o755); err != nil {
		t.Fatalf("write fake deepseek-tui: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "say hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" || result.Output != "Hi there!" {
			t.Fatalf("result = %+v, want completed Hi there!", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestDeepseekBackendExecFailure(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "deepseek-tui")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekExecScript("failure")), 0o755); err != nil {
		t.Fatalf("write fake deepseek-tui: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "no API key") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestDeepseekBackendInvokesCurrentExecJSONCommand(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	workDir := filepath.Join(tempDir, "project")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	fakePath := filepath.Join(tempDir, "deepseek-tui")
	if err := os.WriteFile(fakePath, []byte(fakeDeepseekExecScript("success")), 0o755); err != nil {
		t.Fatalf("write fake deepseek-tui: %v", err)
	}

	backend, err := New("DeepSeek-TUI", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEEPSEEK_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new deepseek backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "say hello", ExecOptions{
		Cwd:        workDir,
		Model:      "deepseek-v4-flash",
		Timeout:    5 * time.Second,
		CustomArgs: []string{"app-server", "--stdio", "--custom-ok"},
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
	wantPrefix := []string{"--workspace", workDir, "exec", "--json", "--auto", "--model", "deepseek-v4-flash"}
	if len(args) < len(wantPrefix)+2 {
		t.Fatalf("args too short: %q", args)
	}
	if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args prefix = %q, want %q", args[:len(wantPrefix)], wantPrefix)
	}
	if slices.Contains(args, "app-server") || slices.Contains(args, "--stdio") {
		t.Fatalf("legacy app-server args leaked into command: %q", args)
	}
	if !slices.Contains(args, "--custom-ok") {
		t.Fatalf("allowed custom arg missing from command: %q", args)
	}
	if args[len(args)-1] != "say hello" {
		t.Fatalf("last arg = %q, want prompt", args[len(args)-1])
	}
}
