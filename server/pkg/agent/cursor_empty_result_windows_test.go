//go:build windows

package agent

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const emptyResultShimHelperEnv = "MULTICA_CURSOR_EMPTY_RESULT_HELPER"

// TestCursorEmptyResultShimHelperProcess is not a test. Re-executed by the fake
// cursor-agent.ps1 below, it emits the failure shape C1 is about: a terminal
// result event marked as an error whose payload is empty, with the only
// actionable detail on stderr. Inert unless the shim env var is set.
func TestCursorEmptyResultShimHelperProcess(t *testing.T) {
	if os.Getenv(emptyResultShimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "helper: drain stdin: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(`{"type":"system","subtype":"init","session_id":"sess-empty-result"}`)
	fmt.Fprintln(os.Stderr, "cursor-agent: provider rejected the request: upstream returned 500")
	fmt.Println(`{"type":"result","subtype":"error","is_error":true,"session_id":"sess-empty-result"}`)
	// Exit before the test framework can print PASS/ok into the JSON stream.
	os.Exit(0)
}

// TestCursorExecuteReportsEmptyErrorResultWithContext is the Windows half of
// the C1 regression: a result that ARRIVES but names nothing is a different
// state from a result that never arrives, and both must carry the captured
// stderr. It runs through the production launch chain
// (Go → powershell -File → native child) so the stderr tail is captured by the
// same path production uses.
func TestCursorExecuteReportsEmptyErrorResultWithContext(t *testing.T) {
	hosts := availablePowerShellHosts()
	if len(hosts) == 0 {
		t.Skip("no PowerShell host available")
	}
	for _, host := range hosts {
		t.Run(filepath.Base(host), func(t *testing.T) {
			stubPowerShell(t, host, true)
			assertEmptyErrorResultContext(t)
		})
	}
}

func assertEmptyErrorResultContext(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary to use as native child: %v", err)
	}

	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0cursor-agent.ps1\" %*\r\n")
	ps1 := fmt.Sprintf(""+
		"$env:%s = '1'\r\n"+
		"& '%s' '-test.run=^TestCursorEmptyResultShimHelperProcess$' '--' $args\r\n"+
		"exit $LASTEXITCODE\r\n",
		emptyResultShimHelperEnv,
		self)
	writeFile(t, filepath.Join(dir, "cursor-agent.ps1"), ps1)

	backend, err := New("cursor", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 60 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	t.Logf("error = %q", result.Error)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
	for _, want := range []string{
		"cursor-agent returned an error result without details",
		"result_seen=true",
		"result_bytes=0",
		"last_event_type=result",
		"cursor stderr:",
		"upstream returned 500",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
}
