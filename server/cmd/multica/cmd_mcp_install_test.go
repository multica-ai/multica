package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMCPInstall_RejectsUnsupportedClient(t *testing.T) {
	cmd := mcpInstallCmd
	if err := cmd.Flags().Set("client", "vscode"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("client", "claude-code") })

	err := runMCPInstall(cmd, nil)
	if err == nil {
		t.Fatal("expected error for unsupported client, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported client") {
		t.Fatalf("error %q should mention 'unsupported client'", err)
	}
}

func TestInstallForClaudeCode_RejectsInvalidScope(t *testing.T) {
	// Force a PATH where 'claude' definitely exists by pointing to a tmp dir
	// containing a stub. We don't need claude to actually run because scope
	// validation happens before the exec call.
	tmp := t.TempDir()
	if err := writeStubClaude(tmp); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", tmp)

	err := installForClaudeCode("multica", "everywhere", "/usr/local/bin/multica")
	if err == nil {
		t.Fatal("expected scope validation error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("error %q should mention 'invalid scope'", err)
	}
}

func TestInstallForClaudeCode_MissingClaudeCLI(t *testing.T) {
	// Empty PATH so 'claude' lookup fails.
	t.Setenv("PATH", "")

	err := installForClaudeCode("multica", "user", "/usr/local/bin/multica")
	if err == nil {
		t.Fatal("expected error when claude CLI is missing, got nil")
	}
	if !strings.Contains(err.Error(), "'claude' CLI not found") {
		t.Fatalf("error %q should mention missing claude CLI", err)
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") {
		t.Fatal("contains() = false, want true for matching element")
	}
	if contains([]string{"a", "b"}, "c") {
		t.Fatal("contains() = true, want false for missing element")
	}
}

// writeStubClaude drops a no-op `claude` executable in dir so exec.LookPath
// finds it. The stub is never executed by these tests — they assert against
// validation that runs before the exec call.
func writeStubClaude(dir string) error {
	return os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
}
