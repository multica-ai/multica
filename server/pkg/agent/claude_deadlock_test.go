package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain intercepts when the test binary is re-executed as a fake
// child process by the agent backend. The fake's behavior is selected via
// CLAUDE_FAKE_MODE; absent that env var, this is a normal `go test` run.
func TestMain(m *testing.M) {
	switch mode := os.Getenv("CLAUDE_FAKE_MODE"); mode {
	case "":
		os.Exit(m.Run())
	case "startup_stdout_burst":
		runFakeClaudeStartupStdoutBurst()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown CLAUDE_FAKE_MODE: %q\n", mode)
		os.Exit(2)
	}
}

// runFakeClaudeStartupStdoutBurst writes ~256 KiB to stdout BEFORE
// reading any byte from stdin, then drains stdin and emits a stream-json
// result. Reproduces the stdio deadlock: if the daemon writes the prompt
// to stdin before a stdout reader is running, the child blocks writing
// stdout and the daemon blocks writing stdin — neither side can progress
// until the per-task context times out and the child is killed.
func runFakeClaudeStartupStdoutBurst() {
	line := strings.Repeat("x", 1020)
	bw := bufio.NewWriter(os.Stdout)
	for i := 0; i < 256; i++ {
		if _, err := fmt.Fprintf(bw, `{"type":"log","log":{"level":"info","message":"%s"}}`+"\n", line); err != nil {
			os.Exit(11)
		}
	}
	if err := bw.Flush(); err != nil {
		os.Exit(12)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(13)
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-deadlock","result":"done"}`)
}

// TestClaudeExecuteDoesNotDeadlockOnStartupStdoutBurst verifies that the
// claude backend drains stdout concurrently with writing the prompt to
// stdin. The buggy path serialises the two: writeClaudeInput runs before
// the reader goroutine starts, so a child that emits startup output
// before its first stdin read deadlocks both directions. Field evidence
// in the daemon log shows tasks failing exactly at the 2 h per-task
// timeout with "write |1: The pipe has been ended.", produced when
// runCtx fires, the child is killed, and the blocked stdin Write
// finally unwinds.
//
// The fake child writes 256 KiB to stdout then 128 KiB of prompt is
// pushed at stdin — both well past any plausible OS pipe buffer
// (Linux ~64 KiB, Windows 4-64 KiB) — so a regression here hangs until
// the test deadline rather than passing slowly.
func TestClaudeExecuteDoesNotDeadlockOnStartupStdoutBurst(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "startup_stdout_burst"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	// 128 KiB prompt forces writeClaudeInput to block until the child
	// drains stdin, which the buggy code cannot reach because the reader
	// goroutine hasn't started yet.
	prompt := strings.Repeat("p", 128*1024)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	go func() {
		for range session.Messages {
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
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for result — claude backend is deadlocked on writeClaudeInput because stdout is not being drained concurrently")
	}
}
