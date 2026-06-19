//go:build unix

package agent

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestCursorExecuteStopsAfterTerminalResult(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-terminal"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-terminal"}'
sleep 10
`
	result := executeFakeCursor(t, script)

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if result.Output != "done" {
		t.Fatalf("output = %q, want done", result.Output)
	}
	if result.SessionID != "sess-terminal" {
		t.Fatalf("session id = %q, want sess-terminal", result.SessionID)
	}
}

func TestCursorExecuteStopsAfterTerminalErrorResult(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-terminal-error"}'
printf '%s\n' '{"type":"result","subtype":"error","is_error":true,"result":"failed hard","session_id":"sess-terminal-error"}'
sleep 10
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	if result.Error != "failed hard" {
		t.Fatalf("error = %q, want failed hard", result.Error)
	}
	if result.Output != "failed hard" {
		t.Fatalf("output = %q, want failed hard", result.Output)
	}
	if result.SessionID != "sess-terminal-error" {
		t.Fatalf("session id = %q, want sess-terminal-error", result.SessionID)
	}
}

func executeFakeCursor(t *testing.T, script string) Result {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	result := <-session.Result
	if result.Status == "timeout" {
		t.Fatalf("cursor backend timed out instead of stopping after terminal result; error=%q", result.Error)
	}
	return result
}
