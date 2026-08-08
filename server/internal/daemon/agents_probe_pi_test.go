//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeAgentCLIsFindsManagedPiAfterUserPathsMiss(t *testing.T) {
	runtimeRoot := t.TempDir()
	t.Setenv("MULTICA_MANAGED_RUNTIME_DIR", runtimeRoot)
	t.Setenv("PATH", "")
	t.Setenv("MULTICA_PI_PATH", "pi")

	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	managedPi := filepath.Join(runtimeRoot, "pi", "current", "pi")
	if err := os.MkdirAll(filepath.Dir(managedPi), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedPi, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entry, ok := probeAgentCLIs()["pi"]
	if !ok {
		t.Fatal("managed Pi was not discovered")
	}
	if entry.Path != managedPi || entry.Command != managedPi {
		t.Fatalf("managed Pi entry = %+v, want path and command %q", entry, managedPi)
	}
}
