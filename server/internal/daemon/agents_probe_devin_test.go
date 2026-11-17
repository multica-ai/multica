package daemon

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestProbeAgentCLIsDiscoversNativeDevin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	writeDaemonTestExecutable(t, filepath.Join(dir, "devin"), []byte("#!/bin/sh\nexit 0\n"))
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)
	t.Setenv("PATH", dir)
	t.Setenv("MULTICA_DEVIN_PATH", "")
	t.Setenv("MULTICA_DEVIN_MODEL", "selected-model")
	entry, ok := probeAgentCLIs()["devin"]
	if !ok || entry.Command != "devin" || entry.Model != "selected-model" {
		t.Fatalf("Devin not discovered as a native family: %+v ok=%v", entry, ok)
	}
	if providerDisplayName("devin") != "Devin" || agent.IsBuiltinRuntime("devin") {
		t.Fatal("Devin must be a native protocol family, not a self-referencing builtin descriptor")
	}
}

func TestDevinRegistrationUsesBinaryVersionGate(t *testing.T) {
	for _, tc := range []struct {
		version string
		allowed bool
	}{
		{"3000.3.21", false}, {"3000.3.22", true}, {"3000.10.21", true}, {"0.0.0-dev", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			fx := newBatchFixture(t)
			fx.probeVersion = tc.version
			checkAgentMinVersion = agent.CheckMinVersion
			fx.daemon.cfg.Agents = map[string]AgentEntry{"devin": {Path: "/fake/devin", Command: "devin"}}
			runtimes, belowMin, _ := fx.daemon.detectBuiltinRuntimes(context.Background())
			if (len(runtimes) == 1) != tc.allowed || (len(belowMin) == 0) != tc.allowed {
				t.Fatalf("version %s: runtimes=%v belowMin=%v", tc.version, runtimes, belowMin)
			}
		})
	}
}
