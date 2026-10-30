package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolveAgentExecutablePath_ProfileOverride is the set-path gate
// regression for MUL-3284 / #7613. appendProfileRuntimes must reuse
// resolveAgentExecutablePath (exec.LookPath) so Windows PATHEXT completion
// and unix exec-bit checks match what agent launch already does — including
// assigning LookPath's return value, not the raw override.
func TestResolveAgentExecutablePath_ProfileOverride(t *testing.T) {
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

		exe := filepath.Join(dir, "tool.exe")
		if err := os.WriteFile(exe, []byte("MZ"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := resolveAgentExecutablePath(exe)
		if err != nil {
			t.Fatalf("resolveAgentExecutablePath(%q): %v", exe, err)
		}
		if got != exe {
			t.Fatalf("resolved .exe = %q, want %q", got, exe)
		}

		cmd := filepath.Join(dir, "tool.cmd")
		if err := os.WriteFile(cmd, []byte("@echo off\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err = resolveAgentExecutablePath(cmd)
		if err != nil {
			t.Fatalf("resolveAgentExecutablePath(%q): %v", cmd, err)
		}
		if got != cmd {
			t.Fatalf("resolved .cmd = %q, want %q", got, cmd)
		}

		// npm-style shim: operator pins C:\tools\openclaw while only
		// openclaw.cmd exists. LookPath must complete the extension.
		shimDir := t.TempDir()
		shimCmd := filepath.Join(shimDir, "openclaw.cmd")
		if err := os.WriteFile(shimCmd, []byte("@echo off\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		extensionless := filepath.Join(shimDir, "openclaw")
		got, err = resolveAgentExecutablePath(extensionless)
		if err != nil {
			t.Fatalf("resolveAgentExecutablePath(%q): %v", extensionless, err)
		}
		if got != shimCmd {
			t.Fatalf("extension-less override resolved to %q, want %q", got, shimCmd)
		}
		// Non-PATHEXT absolute files are not asserted: on Windows,
		// exec.LookPath accepts an absolute path to any existing file.
	} else {
		tool := filepath.Join(dir, "tool")
		if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveAgentExecutablePath(tool); err == nil {
			t.Fatalf("0644 file should not resolve on unix")
		}
		if err := os.Chmod(tool, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveAgentExecutablePath(tool)
		if err != nil {
			t.Fatalf("resolveAgentExecutablePath(%q): %v", tool, err)
		}
		if got != tool {
			t.Fatalf("resolved unix tool = %q, want %q", got, tool)
		}
	}

	if _, err := resolveAgentExecutablePath(dir); err == nil {
		t.Fatalf("directory should not resolve")
	}
	if _, err := resolveAgentExecutablePath(filepath.Join(dir, "missing")); err == nil {
		t.Fatalf("missing path should not resolve")
	}
}
