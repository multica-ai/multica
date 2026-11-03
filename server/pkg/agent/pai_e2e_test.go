package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPaiE2ERealBinary drives the real `p-ai acp` binary through the full
// paiBackend lifecycle — initialize → session/new → (set_model) →
// session/prompt → streamed turn → Result — against P-ai's real configuration
// and its real LLM providers. It is opt-in only, because it consumes real
// model quota and requires a signed-in P-ai install:
//
//	PAI_E2E=1 \
//	PAI_E2E_BIN=/path/to/p-ai(.exe) \
//	[PAI_E2E_MODEL=<model id from P-ai's catalog>] \
//	[PAI_E2E_CWD=<working directory>] \
//	go test -v -count=1 -run TestPaiE2ERealBinary ./pkg/agent/
//
// Without PAI_E2E=1 the test no-ops, so the normal `go test ./...` suite
// never touches the network or the user's P-ai configuration.
func TestPaiE2ERealBinary(t *testing.T) {
	if os.Getenv("PAI_E2E") != "1" {
		t.Skip("opt-in: set PAI_E2E=1 and PAI_E2E_BIN to run against the real p-ai binary")
	}
	binPath := os.Getenv("PAI_E2E_BIN")
	if binPath == "" {
		t.Fatal("PAI_E2E=1 requires PAI_E2E_BIN pointing at the real p-ai binary")
	}
	if _, err := os.Stat(binPath); err != nil {
		t.Fatalf("PAI_E2E_BIN %q: %v", binPath, err)
	}

	cwd := os.Getenv("PAI_E2E_CWD")
	if cwd == "" {
		cwd = t.TempDir()
	}

	// Log to stdout (not stderr) so `go test -v` streams daemon-side log
	// lines live into the terminal — useful as recorded evidence. When
	// PAI_E2E_LOG is set, everything is also written to that file, so a run
	// inside a visible console window still leaves a textual record.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logSink := io.Writer(os.Stdout)
	if p := os.Getenv("PAI_E2E_LOG"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			t.Fatalf("open PAI_E2E_LOG %q: %v", p, err)
		}
		defer f.Close()
		logSink = io.MultiWriter(os.Stdout, f)
	}
	say := func(format string, args ...any) {
		fmt.Fprintf(logSink, format+"\n", args...)
	}
	backend, err := New("pai", Config{
		ExecutablePath: binPath,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("new pai backend: %v", err)
	}

	opts := ExecOptions{
		Cwd: cwd,
		// Mirrors the daemon's providerNeedsInlineSystemPrompt path for pai.
		SystemPrompt: "runtime-brief: you are executing a task on behalf of Multica. Answer directly.",
		Timeout:      8 * time.Minute,
	}
	if m := os.Getenv("PAI_E2E_MODEL"); m != "" {
		opts.Model = m
	}

	const prompt = "E2E connectivity check: reply with exactly one line 'E2E-OK' and nothing else. Do not use any tools."

	say("=== [pai-e2e] launching real p-ai binary: %s (cwd=%s, model=%q) ===", binPath, cwd, opts.Model)
	session, err := backend.Execute(context.Background(), prompt, opts)
	if err != nil {
		say("=== [pai-e2e] execute failed: %v ===", err)
		t.Fatalf("execute: %v", err)
	}

	go func() {
		for m := range session.Messages {
			switch m.Type {
			case MessageText:
				fmt.Printf("[pai-e2e][text] %s\n", m.Content)
			case MessageThinking:
				fmt.Printf("[pai-e2e][thinking] %s\n", m.Content)
			case MessageToolUse:
				fmt.Printf("[pai-e2e][tool-use] %s input=%v\n", m.Tool, m.Input)
			case MessageToolResult:
				fmt.Printf("[pai-e2e][tool-result] %s output=%.200s\n", m.Tool, m.Output)
			case MessageStatus:
				fmt.Printf("[pai-e2e][status] %s (session=%s)\n", m.Status, m.SessionID)
			case MessageError:
				fmt.Printf("[pai-e2e][error] %s\n", m.Content)
			default:
				fmt.Printf("[pai-e2e][%s] %s\n", m.Type, m.Content)
			}
		}
	}()

	select {
	case result := <-session.Result:
		say("=== [pai-e2e] result: status=%s session=%s duration=%dms usage=%v error=%q ===",
			result.Status, result.SessionID, result.DurationMs, result.Usage, result.Error)
		say("=== [pai-e2e] output: %s ===", result.Output)
		if result.Status != "completed" {
			t.Fatalf("status = %q, error = %q", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, "E2E-OK") {
			t.Errorf("output = %q, want it to contain E2E-OK", result.Output)
		}
	case <-time.After(10 * time.Minute):
		say("=== [pai-e2e] timeout waiting for result ===")
		t.Fatal("timeout waiting for result")
	}
}
