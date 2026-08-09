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

const (
	piShimHelperEnv      = "MULTICA_PI_SHIM_HELPER"
	piShimHelperArgvFile = "MULTICA_PI_SHIM_ARGV_FILE"
	piShimHelperInFile   = "MULTICA_PI_SHIM_STDIN_FILE"
)

// TestPiShimHelperProcess is re-executed by the fake pi.ps1 as its native
// child. It records the argv and stdin that made it through PowerShell, emits a
// successful Pi event stream, and exits before the Go test framework writes to
// stdout.
func TestPiShimHelperProcess(t *testing.T) {
	if os.Getenv(piShimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}

	var forwarded []string
	for i, arg := range os.Args {
		if arg == "--" {
			forwarded = os.Args[i+1:]
			break
		}
	}
	if err := os.WriteFile(os.Getenv(piShimHelperArgvFile), []byte(strings.Join(forwarded, "\n")), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
		os.Exit(1)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(piShimHelperInFile), stdin, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write stdin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`{"type":"agent_start"}`)
	fmt.Println(`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2}}}`)
	os.Exit(0)
}

// TestPiExecutePromptSurvivesPowerShellShim exercises the complete Windows
// production boundary that #6457 crosses:
//
//	Go -> powershell -File pi.ps1 -> native child
//
// The prompt must be absent from the argv PowerShell re-serialises and must be
// inherited by the native child on stdin. Every available PowerShell host is
// exercised because Windows PowerShell 5.1 uses the Legacy argument mode that
// exposed the bug, while current pwsh uses Standard mode.
func TestPiExecutePromptSurvivesPowerShellShim(t *testing.T) {
	hosts := availablePowerShellHosts()
	if len(hosts) == 0 {
		t.Skip("no PowerShell host available")
	}
	for _, host := range hosts {
		t.Run(filepath.Base(host), func(t *testing.T) {
			stubPowerShell(t, host, true)
			assertPiPromptSurvivesShim(t)
		})
	}
}

func assertPiPromptSurvivesShim(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")
	sessionPath := filepath.Join(dir, "session.jsonl")

	cmdPath := filepath.Join(dir, "pi.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0pi.ps1\" %*\r\n")
	ps1 := fmt.Sprintf(""+
		"$env:%s = '1'\r\n"+
		"$env:%s = '%s'\r\n"+
		"$env:%s = '%s'\r\n"+
		"& '%s' '-test.run=^TestPiShimHelperProcess$' '--' $args\r\n"+
		"exit $LASTEXITCODE\r\n",
		piShimHelperEnv,
		piShimHelperArgvFile, argvPath,
		piShimHelperInFile, stdinPath,
		self)
	writeFile(t, filepath.Join(dir, "pi.ps1"), ps1)

	prompt := "MULTICA_AGENT_BUILDER_INPUT\n" +
		`{"instructions":"Run go build -ldflags \"-X main.version=foo\"","description":"- local work"}`
	backend, err := New("pi", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(pi): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Timeout:         60 * time.Second,
		ResumeSessionID: sessionPath,
		Model:           "cpa/grok-4.5-high",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("native child never recorded argv: %v; result=%+v", err, result)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("native child never recorded stdin: %v; result=%+v", err, result)
	}
	for _, arg := range strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n") {
		for _, needle := range []string{"MULTICA_AGENT_BUILDER_INPUT", "instructions", "-X", "local work"} {
			if strings.Contains(arg, needle) {
				t.Errorf("prompt fragment %q leaked into native child argv element %q", needle, arg)
			}
		}
	}
	gotArgv := string(argvRaw)
	for _, want := range []string{"-p", "--mode", "json", "--session", "--provider", "cpa", "--model", "grok-4.5-high"} {
		if !strings.Contains(gotArgv, want) {
			t.Errorf("expected %q to reach native child; argv=%q", want, gotArgv)
		}
	}
	if string(stdinRaw) != prompt {
		t.Errorf("prompt did not survive Go -> PowerShell -> native child:\n got  %q\n want %q", string(stdinRaw), prompt)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}
