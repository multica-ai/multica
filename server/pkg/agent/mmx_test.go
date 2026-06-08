package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietMmxLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewReturnsMmxBackend(t *testing.T) {
	t.Parallel()

	b, err := New("mmx", Config{ExecutablePath: "/nonexistent/mmx"})
	if err != nil {
		t.Fatalf("New(mmx) error: %v", err)
	}
	if _, ok := b.(*mmxBackend); !ok {
		t.Fatalf("expected *mmxBackend, got %T", b)
	}
}

func TestMmxLaunchHeader(t *testing.T) {
	t.Parallel()

	if got := LaunchHeader("mmx"); got != "mmx text chat (json)" {
		t.Errorf("unexpected launch header: %q", got)
	}
}

func TestMmxCapability(t *testing.T) {
	t.Parallel()

	cap := CapabilityOrDefault("mmx")
	if cap.StreamDisplay || cap.ToolCallStream || cap.Approval || cap.ResumeSession || cap.PlanMode || !cap.StructuredOutput {
		t.Fatalf("unexpected mmx capability: %+v", cap)
	}
}

func TestBuildMmxArgsBasic(t *testing.T) {
	t.Parallel()

	args := buildMmxArgs(ExecOptions{
		Cwd:         "/work",
		Model:       "MiniMax-M2.7",
		SystemPrompt: "answer briefly",
	}, "/tmp/msg.json")

	want := []string{
		"text", "chat",
		"--messages-file", "/tmp/msg.json",
		"--output", "json",
		"--model", "MiniMax-M2.7",
		"--system", "answer briefly",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildMmxArgs mismatch\n got: %v\nwant: %v", args, want)
	}
}

func TestBuildMmxArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildMmxArgs(ExecOptions{
		ExtraArgs: []string{
			"--output", "text",
			"--temperature", "0.7",
		},
		CustomArgs: []string{
			"--model", "bad-model",
			"--api-key", "stolen-key",
			"--region", "us",
			"--verbose",
		},
	}, "/tmp/msg.json")

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "bad-model") {
		t.Errorf("blocked --model should not pass through: %v", args)
	}
	if strings.Contains(joined, "stolen-key") {
		t.Errorf("blocked --api-key should not pass through: %v", args)
	}
	if strings.Contains(joined, "--output text") {
		t.Errorf("blocked --output value leaked through: %v", args)
	}
	// Verify --output value is "json" not "text".
	for i, a := range args {
		if a == "--output" && i+1 < len(args) && args[i+1] != "json" {
			t.Errorf("--output should be 'json', got %q", args[i+1])
		}
	}
	if !strings.Contains(joined, "--temperature 0.7") {
		t.Errorf("non-blocked extra arg should pass through: %v", args)
	}
	if !strings.Contains(joined, "--verbose") {
		t.Errorf("non-blocked custom arg should pass through: %v", args)
	}
}

func TestMmxExecuteExecutableNotFound(t *testing.T) {
	t.Parallel()

	b := &mmxBackend{cfg: Config{ExecutablePath: "/nonexistent/mmx", Logger: quietMmxLogger()}}
	_, err := b.Execute(context.Background(), "hello", ExecOptions{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected error for missing executable")
	}
	if !strings.Contains(err.Error(), "mmx executable not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMmxAuthErrorDetection(t *testing.T) {
	t.Parallel()

	authErr := &mmxExecError{Code: 3, Message: "API key rejected (HTTP 401).", ExitErr: io.ErrUnexpectedEOF}
	if !isMmxAuthError(authErr) {
		t.Fatal("expected auth error to be detected")
	}
	if isMmxRateLimit(authErr) {
		t.Fatal("auth error should not be rate limit")
	}
}

func TestMmxRateLimitDetection(t *testing.T) {
	t.Parallel()

	rateErr := &mmxExecError{Code: 4, Message: "rate limit exceeded (HTTP 429).", ExitErr: io.ErrUnexpectedEOF}
	if !isMmxRateLimit(rateErr) {
		t.Fatal("expected rate limit error to be detected")
	}
	if isMmxAuthError(rateErr) {
		t.Fatal("rate limit error should not be auth")
	}
}

func TestMmxExecuteWithFakeCLI(t *testing.T) {
	t.Parallel()

	// Fake mmx that returns a valid Anthropic Messages API response.
	response := mmxResponse{
		ID:         "msg_1",
		Type:       "message",
		Role:       "assistant",
		Model:      "MiniMax-M2.7",
		StopReason: "end_turn",
		Content: []mmxContentBlock{
			{Type: "text", Text: "hello world"},
		},
		Usage: mmxUsage{InputTokens: 10, OutputTokens: 5},
	}
	respJSON, _ := json.Marshal(response)

	// Write the response JSON to a separate file, then cat it from the script
	// to avoid shell interpretation of JSON content.
	execPath := filepath.Join(t.TempDir(), "mmx")
	respPath := filepath.Join(t.TempDir(), "response.json")
	if err := os.WriteFile(respPath, respJSON, 0o644); err != nil {
		t.Fatalf("write response file: %v", err)
	}
	script := "#!/bin/sh\ncat " + respPath + "\n"
	writeTestExecutable(t, execPath, []byte(script))

	backend := &mmxBackend{cfg: Config{ExecutablePath: execPath, Logger: quietMmxLogger()}}
	session, err := backend.Execute(context.Background(), "hello", ExecOptions{
		Timeout: 5 * time.Second,
		Model:   "MiniMax-M2.7",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var streamed strings.Builder
	for msg := range session.Messages {
		if msg.Type == MessageText {
			streamed.WriteString(msg.Content)
		}
	}
	result := <-session.Result

	if result.Status != "completed" {
		t.Fatalf("expected completed, got %q: %s", result.Status, result.Error)
	}
	if result.Output != "hello world" {
		t.Fatalf("output = %q, want 'hello world'", result.Output)
	}
	if streamed.String() != "hello world" {
		t.Fatalf("streamed = %q, want 'hello world'", streamed.String())
	}
	if result.Usage["MiniMax-M2.7"].InputTokens != 10 || result.Usage["MiniMax-M2.7"].OutputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestMmxExecuteWithFakeCLIErrors(t *testing.T) {
	t.Parallel()

	// Fake mmx that returns a structured error.
	execPath := filepath.Join(t.TempDir(), "mmx")
	writeTestExecutable(t, execPath, []byte(`#!/bin/sh
printf '{"error":{"code":3,"message":"API key rejected (HTTP 401).","hint":"Run mmx auth login"}}'
exit 1
`))

	backend := &mmxBackend{cfg: Config{ExecutablePath: execPath, Logger: quietMmxLogger()}}
	session, err := backend.Execute(context.Background(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {}
	result := <-session.Result

	if result.Status != "failed" {
		t.Fatalf("expected failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "authentication error") && !strings.Contains(result.Error, "API key rejected") {
		t.Fatalf("expected auth-related error, got: %s", result.Error)
	}
}

func TestMmxTruncate(t *testing.T) {
	t.Parallel()

	if got := truncate("short", 10); got != "short" {
		t.Fatalf("expected 'short', got %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := truncate(long, 100); len(got) != 103 { // 100 + "..."
		t.Fatalf("expected len 103, got %d", len(got))
	}
}
