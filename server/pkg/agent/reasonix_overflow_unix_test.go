//go:build unix

package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// overflowScriptWithGiantLine reuses the known-good effort stub handshake and
// injects one oversized line between the turn chunk and the prompt result.
// The size comes from OVERFLOW_MIB so one stub covers both the below-cap and
// the over-cap cases.
func overflowScriptWithGiantLine() string {
	script := reasonixEffortExecStub(reasonixEffortSessionResult)
	idx := strings.Index(script, "stopReason")
	if idx < 0 {
		panic("effort stub has no prompt result line")
	}
	lineStart := strings.LastIndex(script[:idx], "\n") + 1
	giant := "      dd if=/dev/zero bs=1048576 count=${OVERFLOW_MIB:-33} 2>/dev/null | tr \"\\000\" \"x\"\n" +
		"      printf \"\\n\"\n"
	return script[:lineStart] + giant + script[lineStart:]
}

func runReasonixOverflowTurn(t *testing.T, mib string) (Result, time.Duration) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("REASONIX_RPC_LOG", filepath.Join(dir, "rpc.log"))
	t.Setenv("OVERFLOW_MIB", mib)
	fakePath := filepath.Join(dir, "reasonix")
	writeTestExecutable(t, fakePath, []byte(overflowScriptWithGiantLine()))

	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	timeout := 45 * time.Second
	session, err := backend.Execute(context.Background(), "ping", ExecOptions{Timeout: timeout})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	start := time.Now()
	result := <-session.Result
	return result, time.Since(start)
}

// An 11 MiB line fits the shared 32 MiB agent-stream bound but exceeds
// reasonix 10 MiB one-off cap: the turn is valid and must complete.
func TestReasonixExecuteCompletesBelowSharedBound(t *testing.T) {
	result, _ := runReasonixOverflowTurn(t, "11")
	if result.Status != "completed" {
		t.Fatalf("result.Status = %q, want completed (error: %q)", result.Status, result.Error)
	}
}

// Past every bound the run terminal state is unknown: it must read as
// failure naming the scanner fault, fast — never as success, never masked
// behind a generic exit message, never hung until Timeout.
func TestReasonixExecuteFailsOnScannerOverflow(t *testing.T) {
	result, elapsed := runReasonixOverflowTurn(t, "33")
	if result.Status == "completed" {
		t.Fatalf("result completed, want failure: stopped scanner leaves terminal state unknown")
	}
	if result.Status != "failed" {
		t.Fatalf("result.Status = %q, want failed (error: %q)", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "too long") {
		t.Fatalf("error = %q, want the scanner fault surfaced, not a generic exit message", result.Error)
	}
	if elapsed >= 45*time.Second {
		t.Fatalf("run hung until Timeout (%v); scanner failure must surface fast", elapsed.Round(time.Second))
	}
}
