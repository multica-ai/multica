//go:build unix

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestCursorExecuteEmitsTerminalResultText(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-result-text"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"final-only answer","session_id":"sess-result-text"}'
`
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

	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range session.Messages {
			messages = append(messages, msg)
		}
	}()

	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if result.Output != "final-only answer" {
		t.Fatalf("output = %q, want final-only answer", result.Output)
	}
	for _, msg := range messages {
		if msg.Type == MessageText && msg.Content == "final-only answer" {
			return
		}
	}
	t.Fatalf("expected terminal result text in message stream, got %+v", messages)
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
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
	if result.SessionID != "sess-terminal-error" {
		t.Fatalf("session id = %q, want sess-terminal-error", result.SessionID)
	}
}

func TestCursorExecuteReportsSanitizedStderrOnProcessFailure(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	script := `#!/bin/sh
dd if=/dev/zero bs=4096 count=1 2>/dev/null | tr '\000' x >&2
printf '\nAuthorization: Bearer cursor-secret-token-value\npath=%s/private\n' "$HOME" >&2
exit 1
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent exited with error: exit status 1",
		"result_seen=false",
		"exit_code=1",
		"cursor stderr:",
		"Authorization: [REDACTED]",
		"actions completed before finalization may already have taken effect",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	for _, secret := range []string{"cursor-secret-token-value", homeDir} {
		if strings.Contains(result.Error, secret) {
			t.Errorf("error leaked %q: %q", secret, result.Error)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
	if len(result.Error) > agentStderrTailBytes+1024 {
		t.Fatalf("error length = %d, want bounded stderr diagnostic", len(result.Error))
	}
}

func TestCursorExecuteReportsMalformedTerminalEvent(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-malformed"}'
printf '%s\n' '{"type":"result","subtype":"success","result":"truncated"'
exit 1
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"result_seen=false",
		"invalid_event_count=1",
		"last_event_type=system",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
}

func TestCursorExecuteReportsScannerOverflow(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-overflow"}'
dd if=/dev/zero bs=1048576 count=11 2>/dev/null | tr '\000' x
printf '\n'
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent stdout read error",
		"token too long",
		"result_seen=false",
		"scanner_error=true",
		"last_event_type=system",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
}

func TestCursorExecuteFailsOnCleanEOFWithoutResult(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-no-result"}'
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"partial answer"}]}}'
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent stream ended without terminal result",
		"result_seen=false",
		"exit_code=0",
		"last_event_type=assistant",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want partial transcript suppressed", result.Output)
	}
}

func TestCursorExecutePreservesStructuredStreamError(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-stream-error"}'
printf '%s\n' '{"type":"error","error":"provider rejected request"}'
exit 1
`
	result := executeFakeCursor(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"provider rejected request",
		"result_seen=false",
		"exit_code=1",
		"last_event_type=error",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
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
