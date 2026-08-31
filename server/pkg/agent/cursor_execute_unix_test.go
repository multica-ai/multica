//go:build unix

package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real cursor-agent reads the prompt from stdin to EOF (see buildCursorArgs).
// A fake that exits without reading it leaves the prompt write racing the
// child's exit: win and the ~64 KiB pipe buffer swallows it, lose and the read
// end is already closed and the write fails with EPIPE.
//
// That matters because writeErr outranks both `exitErr` and the generic
// "stream ended without terminal result" when finalizing the error (cursor.go),
// so a lost race replaces the failure the test is asserting on with
// "cursor-agent prompt write failed: broken pipe" — the flake that turned main
// red on 2026-07-30 (MUL-5536).
//
// Draining stdin first makes the fake honour the same contract as the real CLI,
// which removes the race instead of papering over it. Only fixtures whose
// expected error ranks below writeErr need it; the scanner-overflow and
// structured-stream-error cases rank above it and are unaffected.
const drainStdin = "cat > /dev/null"

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
` + drainStdin + `
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
	if strings.Contains(result.Error, "cursor-secret-token-value") {
		t.Errorf("error leaked the bearer token: %q", result.Error)
	}
	// Host paths are deliberately NOT masked: a crash diagnostic is only
	// actionable if the path it names is the real one, and masking a path
	// segment never was an access-control boundary. See redact.Text.
	if !strings.Contains(result.Error, homeDir+"/private") {
		t.Errorf("error = %q, want the home path preserved verbatim", result.Error)
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
	// Not t.Parallel(): the fixture streams ~agentStreamMaxLineBytes of
	// subprocess stdout. Under the package default -parallel that competes
	// with siblings for memory and /tmp IO; a tight execute timeout then
	// flakes on CI (VAN-127). Scanner overflow semantics are also covered
	// without subprocess cost in stream_scanner_test.go; this test pins the
	// cursor execute error surface only.
	//
	// The oversized event is sized from agentStreamMaxLineBytes so raising
	// the shared cap cannot silently turn this into a plain oversized-line
	// pass that never reaches the overflow branch. Pre-generating the payload
	// in Go and cat-ing it avoids the dd|tr pipeline that amplified the
	// flake under parallel load.
	overflowPath := filepath.Join(t.TempDir(), "overflow.line")
	writeScannerOverflowLine(t, overflowPath, agentStreamMaxLineBytes+1)
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"type":"system","subtype":"init","session_id":"sess-overflow"}'
cat %q
printf '\n'
`, overflowPath)
	result := executeFakeCursor(t, script, 30*time.Second)

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
` + drainStdin + `
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

// writeScannerOverflowLine writes size bytes without a trailing newline.
func writeScannerOverflowLine(tb testing.TB, path string, size int) {
	tb.Helper()
	if size <= 0 {
		tb.Fatalf("overflow line size = %d, want > 0", size)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		tb.Fatalf("open overflow line %s: %v", path, err)
	}
	chunk := make([]byte, 1024*1024)
	for i := range chunk {
		chunk[i] = 'x'
	}
	remaining := size
	for remaining > 0 {
		n := remaining
		if n > len(chunk) {
			n = len(chunk)
		}
		if _, err := f.Write(chunk[:n]); err != nil {
			_ = f.Close()
			tb.Fatalf("write overflow line %s: %v", path, err)
		}
		remaining -= n
	}
	if err := f.Close(); err != nil {
		tb.Fatalf("close overflow line %s: %v", path, err)
	}
}

func executeFakeCursor(t *testing.T, script string, timeout ...time.Duration) Result {
	t.Helper()

	execTimeout := 5 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		execTimeout = timeout[0]
	}

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: execTimeout})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	result := <-session.Result
	if result.Status == "timeout" {
		t.Fatalf("cursor backend timed out instead of stopping after terminal result; error=%q", result.Error)
	}
	return result
}
