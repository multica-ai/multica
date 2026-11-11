package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDefaultAgentCommandNamesIncludeZcode keeps the shell-fallback probe
// list in lockstep with the MULTICA_ZCODE_PATH probe: without the bare
// command name, a zcode CLI on PATH would only be found via the env override.
func TestDefaultAgentCommandNamesIncludeZcode(t *testing.T) {
	for _, name := range defaultAgentCommandNames {
		if name == "zcode" {
			return
		}
	}
	t.Fatal(`defaultAgentCommandNames does not include "zcode"`)
}

// TestProbeAgentCLIsDiscoversZcodeViaEnvOverride covers the ZCode probe:
// a fake binary wired through MULTICA_ZCODE_PATH registers the "zcode"
// provider, and MULTICA_ZCODE_MODEL seeds the daemon-wide default model —
// ZCode supports per-turn modelSelection on session/send, so unlike zeroclaw
// its probe reads a model env var (see agents_probe.go).
func TestProbeAgentCLIsDiscoversZcodeViaEnvOverride(t *testing.T) {
	fakeDir := t.TempDir()
	name := "zcode-fake"
	content := []byte("#!/bin/sh\nexit 0\n")
	if runtime.GOOS == "windows" {
		name = "zcode-fake.cmd"
		content = []byte("@echo off\r\nexit /b 0\r\n")
	}
	executable := filepath.Join(fakeDir, name)
	if err := os.WriteFile(executable, content, 0o755); err != nil {
		t.Fatal(err)
	}

	origResolve := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = origResolve })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", t.TempDir())
	t.Setenv("MULTICA_ZCODE_PATH", executable)
	t.Setenv("MULTICA_ZCODE_MODEL", "zai/glm-5.3")

	entry, ok := probeAgentCLIs()["zcode"]
	if !ok {
		t.Fatal("zcode was not discovered via MULTICA_ZCODE_PATH")
	}
	if filepath.Clean(entry.Path) != filepath.Clean(executable) {
		t.Fatalf("zcode path = %q, want %q", entry.Path, executable)
	}
	// With a MULTICA_*_PATH override, Command mirrors the override value (see
	// probe()'s envOrDefault), unlike the bare-command-name path used by the
	// omp/pi test.
	if entry.Command != executable {
		t.Fatalf("zcode command = %q, want %q", entry.Command, executable)
	}
	if entry.Model != "zai/glm-5.3" {
		t.Fatalf("zcode model = %q, want %q", entry.Model, "zai/glm-5.3")
	}
}
