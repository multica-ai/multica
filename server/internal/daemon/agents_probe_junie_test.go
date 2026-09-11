package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestProbeAgentCLIsJunie pins the three public pieces of Junie discovery:
// the default command name, the executable-path override, and the independent
// default-model override. Model IDs are intentionally treated as opaque values
// because custom local Junie providers use structured IDs Multica does not own.
func TestProbeAgentCLIsJunie(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture is Unix-only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "custom-junie")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_JUNIE_PATH", path)
	t.Setenv("MULTICA_JUNIE_MODEL", "v1:opaque:local:model")
	// probeAgentCLIs performs an active compatibility handshake for DSH after
	// resolving it. Pin a hard miss so this Junie-only test remains hermetic
	// under the repository's agent-CLI invocation guard.
	t.Setenv("MULTICA_DSH_PATH", filepath.Join(dir, "missing-dsh"))

	entry, ok := probeAgentCLIs()["junie"]
	if !ok {
		t.Fatal("Junie was not discovered from MULTICA_JUNIE_PATH")
	}
	if entry.Path != path || entry.Command != path {
		t.Fatalf("Junie executable = %#v, want path and command %q", entry, path)
	}
	if entry.Model != "v1:opaque:local:model" {
		t.Fatalf("Junie model = %q, want opaque override preserved", entry.Model)
	}
}
