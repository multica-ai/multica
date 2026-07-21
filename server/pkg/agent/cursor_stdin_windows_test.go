//go:build windows

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

// TestCursorExecutePromptSurvivesPowerShellShim is the Windows half of the
// #5649 regression. It drives a real PowerShell host through the same
// .cmd → `powershell -File <ps1>` rewrite production uses, and asserts the two
// properties the fix depends on:
//
//   - the prompt is absent from the argv PowerShell hands the shim, so the
//     `& node.exe index.js $args` re-serialisation in the official shim has
//     nothing user-controlled left to re-tokenise; and
//   - the prompt still arrives byte-for-byte on stdin.
//
// Unlike the unix tests this one needs a real powershell.exe/pwsh.exe, so it
// only runs on Windows. It deliberately does NOT stub powerShellLookup — the
// point is to exercise the actual host.
func TestCursorExecutePromptSurvivesPowerShellShim(t *testing.T) {
	if _, ok := defaultPowerShellLookup(); !ok {
		t.Skip("no PowerShell host available")
	}

	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	// The .cmd only has to exist and carry the right extension; the rewrite
	// routes around it to the sibling .ps1, which is what actually runs.
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0cursor-agent.ps1\" %*\r\n")

	// Record argv and stdin with .NET APIs so nothing adds a BOM or rewrites
	// line endings, then emit the terminal stream-json event.
	ps1 := fmt.Sprintf(""+
		"[System.IO.File]::WriteAllLines('%s', [string[]]$args)\r\n"+
		"$stdin = [Console]::In.ReadToEnd()\r\n"+
		"[System.IO.File]::WriteAllText('%s', $stdin)\r\n"+
		"Write-Output '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\"}'\r\n",
		argvPath, stdinPath)
	writeFile(t, filepath.Join(dir, "cursor-agent.ps1"), ps1)

	prompt := "Please fix the build.\n" +
		`go build -ldflags "-X main.version=foo -X main.commit=bar" -o bin/server ./cmd/server` + "\n" +
		"Thanks."

	backend, err := New("cursor", Config{ExecutablePath: cmdPath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), prompt, ExecOptions{Timeout: 60 * time.Second})
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
		t.Fatalf("read recorded argv (shim did not run?): %v; result=%+v", err, result)
	}
	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read recorded stdin: %v; result=%+v", err, result)
	}

	// PowerShell hands the script one argv element per line here; no element
	// may carry any part of the prompt.
	for _, a := range strings.Split(strings.TrimSuffix(string(argvRaw), "\r\n"), "\r\n") {
		for _, needle := range []string{"-X", "ldflags", "main.version", "Please fix"} {
			if strings.Contains(a, needle) {
				t.Errorf("prompt fragment %q leaked into argv element %q", needle, a)
			}
		}
	}

	// [Console]::In normalises the trailing newline of the stream, so compare
	// on normalised line endings rather than raw bytes.
	gotStdin := strings.ReplaceAll(string(stdinRaw), "\r\n", "\n")
	if strings.TrimRight(gotStdin, "\r\n") != strings.TrimRight(prompt, "\n") {
		t.Errorf("prompt did not survive the PowerShell hop:\n got  %q\n want %q", gotStdin, prompt)
	}

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
}
