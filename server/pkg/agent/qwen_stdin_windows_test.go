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
	qwenShimHelperEnv      = "MULTICA_QWEN_SHIM_HELPER"
	qwenShimHelperArgvFile = "MULTICA_QWEN_SHIM_ARGV_FILE"
	qwenShimHelperInFile   = "MULTICA_QWEN_SHIM_STDIN_FILE"
)

// TestQwenShimHelperProcess is re-executed by the fake qwen.ps1 as its native
// child. It records the argv and stdin that made it through PowerShell, emits a
// successful Qwen event stream, and exits before the Go test framework writes
// its own output to stdout.
func TestQwenShimHelperProcess(t *testing.T) {
	if os.Getenv(qwenShimHelperEnv) != "1" {
		t.Skip("helper process; only runs when re-executed by the shim")
	}

	var forwarded []string
	for i, arg := range os.Args {
		if arg == "--" {
			forwarded = os.Args[i+1:]
			break
		}
	}
	if err := os.WriteFile(os.Getenv(qwenShimHelperArgvFile), []byte(strings.Join(forwarded, "\n")), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write argv: %v\n", err)
		os.Exit(1)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: read stdin: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv(qwenShimHelperInFile), stdin, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "helper: write stdin: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`{"type":"system","subtype":"init","session_id":"sess-qwen-windows","model":"qwen-test"}`)
	fmt.Println(`{"type":"result","subtype":"success","session_id":"sess-qwen-windows","is_error":false,"result":"PONG"}`)
	os.Exit(0)
}

// TestQwenExecutePromptSurvivesPowerShellShim exercises the complete Windows
// production boundary behind #6082:
//
//	Go -> powershell -File qwen.ps1 -> native child
//
// The prompt must be absent from the argv PowerShell re-serialises and must be
// inherited by the native child on stdin without byte changes. Every available
// PowerShell host is exercised because Windows PowerShell 5.1 and current pwsh
// use different native argument-passing modes.
func TestQwenExecutePromptSurvivesPowerShellShim(t *testing.T) {
	hosts := availablePowerShellHosts()
	if len(hosts) == 0 {
		t.Skip("no PowerShell host available")
	}
	for _, host := range hosts {
		t.Run(filepath.Base(host), func(t *testing.T) {
			stubPowerShell(t, host, true)
			assertQwenPromptSurvivesShim(t)
		})
	}
}

func assertQwenPromptSurvivesShim(t *testing.T) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	cmdPath := filepath.Join(dir, "qwen.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0qwen.ps1\" %*\r\n")
	ps1 := fmt.Sprintf(""+
		"$env:%s = '1'\r\n"+
		"$env:%s = '%s'\r\n"+
		"$env:%s = '%s'\r\n"+
		"& '%s' '-test.run=^TestQwenShimHelperProcess$' '--' $args\r\n"+
		"exit $LASTEXITCODE\r\n",
		qwenShimHelperEnv,
		qwenShimHelperArgvFile, argvPath,
		qwenShimHelperInFile, stdinPath,
		self)
	writeFile(t, filepath.Join(dir, "qwen.ps1"), ps1)

	prompt := "First line with \"double quotes\".\n" +
		"Unicode: 你好，世界 — symbols: & 100% ^done"
	backend, err := New("qwen", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(qwen): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Model:   "qwen-test",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitQwenResult(t, session)

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("native child never recorded argv: %v; result=%+v", err, result)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("native child never recorded stdin: %v; result=%+v", err, result)
	}
	argv := strings.Split(strings.TrimSuffix(string(argvRaw), "\n"), "\n")
	for _, arg := range argv {
		for _, fragment := range []string{"First line", "double quotes", "你好", "100%", "^done"} {
			if strings.Contains(arg, fragment) {
				t.Errorf("prompt fragment %q leaked into native child argv element %q", fragment, arg)
			}
		}
	}
	if idx := prefixIndex(argv, []string{"--output-format", "stream-json"}); idx < 0 {
		t.Errorf("stream-json protocol flags did not reach native child; argv=%q", argv)
	}
	if idx := prefixIndex(argv, []string{"--model", "qwen-test"}); idx < 0 {
		t.Errorf("managed model flags did not reach native child; argv=%q", argv)
	}
	if idx := prefixIndex(argv, []string{"--yolo"}); idx < 0 {
		t.Errorf("managed permission flag did not reach native child; argv=%q", argv)
	}
	if idx := prefixIndex(argv, []string{"-p"}); idx >= 0 {
		t.Errorf("prompt flag must stay out of argv; argv=%q", argv)
	}
	if string(stdinRaw) != prompt {
		t.Errorf("prompt did not survive Go -> PowerShell -> native child:\n got  %q\n want %q", string(stdinRaw), prompt)
	}
	if result.Status != "completed" || result.Output != "PONG" || result.SessionID != "sess-qwen-windows" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
