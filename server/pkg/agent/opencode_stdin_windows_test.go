//go:build windows

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpencodeExecuteOversizedPromptStartsOnWindows is the platform regression
// for #6538. It crosses the boundary the bug lived on for real:
//
//	Go os/exec -> CreateProcess -> native opencode.exe
//
// CreateProcess caps lpCommandLine at 32,767 characters and rejects anything
// longer with ERROR_FILENAME_EXCED_RANGE (206), which Go surfaces as the
// misleading "The filename or extension is too long". A prompt carrying a
// workspace's models and skills clears that ceiling on its own, so while the
// prompt was an argv element no such task could start at all. Delivering it on
// stdin removes the ceiling: the process must start, argv must stay free of the
// prompt, and the full payload must arrive on stdin byte-for-byte.
//
// No shim is involved — a Chocolatey-installed opencode.exe is a real PE
// binary, so this is the unmediated CreateProcess path.
func TestOpencodeExecuteOversizedPromptStartsOnWindows(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv.txt")
	stdinPath := filepath.Join(dir, "stdin.txt")

	// Copy the test binary in as the native opencode.exe the backend spawns.
	fakePath := filepath.Join(dir, "opencode.exe")
	selfBytes, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}
	writeTestExecutable(t, fakePath, selfBytes)

	// Past the 32,767-character CreateProcess ceiling, in the same shape as the
	// reported 39,629-byte agent-builder payload.
	prompt := "MULTICA_AGENT_BUILDER_INPUT\n" +
		strings.Repeat(`{"available_workspace_skills":[{"description":"- inspect local changes"}]}`+"\n", 600)
	if len(prompt) <= 32767 {
		t.Fatalf("test prompt must exceed the CreateProcess limit, got %d bytes", len(prompt))
	}

	backend, err := New("opencode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env: map[string]string{
			opencodeStdinHelperEnv:      "1",
			opencodeStdinHelperArgvFile: argvPath,
			opencodeStdinHelperInFile:   stdinPath,
		},
	})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}

	session, err := backend.Execute(t.Context(), prompt, ExecOptions{
		Timeout: 60 * time.Second,
		Model:   "lanz/Lanz-Medium",
	})
	// Before the fix this is exactly where the run died, with
	// "start opencode: fork/exec ...: The filename or extension is too long".
	if err != nil {
		t.Fatalf("Execute failed to start the process with a %d-byte prompt: %v", len(prompt), err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	stdinRaw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("native child never recorded stdin: %v; result=%+v", err, result)
	}
	if string(stdinRaw) != prompt {
		t.Errorf("oversized prompt corrupted on stdin: got %d bytes, want %d", len(stdinRaw), len(prompt))
	}

	argvRaw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("native child never recorded argv: %v; result=%+v", err, result)
	}
	gotArgv := string(argvRaw)
	for _, needle := range []string{"MULTICA_AGENT_BUILDER_INPUT", "available_workspace_skills", "inspect local"} {
		if strings.Contains(gotArgv, needle) {
			t.Errorf("prompt fragment %q leaked into argv: %q", needle, gotArgv)
		}
	}
	if len(gotArgv) > 32767 {
		t.Errorf("argv is %d chars, still past the CreateProcess ceiling", len(gotArgv))
	}
	for _, want := range []string{"run", "--format", "json", "--dangerously-skip-permissions", "--model", "lanz/Lanz-Medium"} {
		if !strings.Contains(gotArgv, want) {
			t.Errorf("expected %q to reach the native child; argv=%q", want, gotArgv)
		}
	}

	if result.Status != "completed" || result.Output != "ok" {
		t.Fatalf("result = %+v, want completed with output ok", result)
	}
}
