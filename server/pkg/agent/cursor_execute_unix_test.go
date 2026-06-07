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

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-terminal"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-terminal"}'
sleep 10
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
		}
		if result.Output != "done" {
			t.Fatalf("output = %q, want done", result.Output)
		}
		if result.SessionID != "sess-terminal" {
			t.Fatalf("session id = %q, want sess-terminal", result.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cursor backend did not stop after terminal result")
	}
}
