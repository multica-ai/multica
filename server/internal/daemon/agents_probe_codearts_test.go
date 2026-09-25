package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeAgentCLIsDiscoversCodeArtsDefaultInstallPath(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, ".codeartsdoer", "installers")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "codearts"
	content := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		name = "codearts.cmd"
		content = []byte("@echo off\r\nexit /b 0\r\n")
	}
	executable := filepath.Join(installDir, name)
	if err := os.WriteFile(executable, content, 0o755); err != nil {
		t.Fatal(err)
	}

	origResolve := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = origResolve })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_CODEARTS_PATH", "")
	t.Setenv("MULTICA_CODEARTS_MODEL", "mimo/mimo-v2.5")

	entry, ok := probeAgentCLIs()["codearts"]
	if !ok {
		t.Fatal("CodeArts was not discovered from its default user install path")
	}
	if filepath.Clean(entry.Path) != filepath.Clean(executable) {
		t.Fatalf("CodeArts path = %q, want %q", entry.Path, executable)
	}
	if entry.Command != "codearts" || entry.Model != "mimo/mimo-v2.5" {
		t.Fatalf("unexpected CodeArts entry: %+v", entry)
	}
	if got := providerDisplayName("codearts"); got != "CodeArts" {
		t.Fatalf("CodeArts display name = %q, want CodeArts", got)
	}
}
