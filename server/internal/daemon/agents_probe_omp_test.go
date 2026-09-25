package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestProbeAgentCLIs_DiscoversPiAndOmpSeparately covers discovery only: it
// places fake `pi` and `omp` binaries on PATH, runs the real probeAgentCLIs(),
// and asserts the descriptor-driven probe loop in agents_probe.go finds two
// separate entries with the right commands.
//
// Discovery is not registration — whether the server is actually told about
// both runtimes is covered by TestRegisterRuntimes_PiAndOmpBothReachTheServer.
func TestProbeAgentCLIs_DiscoversPiAndOmpSeparately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakeDir := t.TempDir()
	for _, name := range []string{"pi", "omp"} {
		fakePath := filepath.Join(fakeDir, name)
		writeDaemonTestExecutable(t, fakePath, []byte("#!/bin/sh\nexit 0\n"))
	}

	// Stub the login-shell resolver so it doesn't try to fork a shell.
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{}
	}
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", fakeDir)
	t.Setenv("MULTICA_PI_PATH", "")
	t.Setenv("MULTICA_OMP_PATH", "")

	agents := probeAgentCLIs()

	// Both pi and omp should be discovered as separate entries.
	piEntry, piOk := agents["pi"]
	if !piOk {
		t.Fatal("pi was not discovered by probeAgentCLIs; expected both pi and omp")
	}
	ompEntry, ompOk := agents["omp"]
	if !ompOk {
		t.Fatal("omp was not discovered by probeAgentCLIs; expected both pi and omp")
	}

	if piEntry.Command != "pi" {
		t.Errorf("pi command = %q, want %q", piEntry.Command, "pi")
	}
	if ompEntry.Command != "omp" {
		t.Errorf("omp command = %q, want %q", ompEntry.Command, "omp")
	}

	// Verify the omp descriptor's display name is what the daemon would
	// surface in the registration payload (detectBuiltinRuntimes calls
	// providerDisplayName for each agent).
	ompDisplayName := providerDisplayName("omp")
	if ompDisplayName != "Oh-My-Pi" {
		t.Errorf("omp display name = %q, want %q", ompDisplayName, "Oh-My-Pi")
	}

	// Verify the pi descriptor is NOT a built-in runtime (it's a protocol
	// family), while omp IS a built-in runtime identity.
	if agent.IsBuiltinRuntime("pi") {
		t.Error("pi should not be a built-in runtime (it is a protocol family)")
	}
	if !agent.IsBuiltinRuntime("omp") {
		t.Error("omp should be a built-in runtime identity")
	}
}

// writeDaemonTestExecutable is a helper for the daemon package's test files.
func writeDaemonTestExecutable(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestRegisterRuntimes_PiAndOmpBothReachTheServer takes the pair the whole
// feature exists for all the way to the wire: real discovery off PATH, then
// the daemon's own registration round, asserting the payload the server
// receives carries both `pi` and `omp` as separate runtimes with their own
// display names.
//
// Discovery finding two entries (TestProbeAgentCLIs_DiscoversPiAndOmpSeparately)
// and New() building two backends both stop short of this: neither runs version
// detection, builds a registration payload, or proves the server ends up with
// two rows. A runtime identity that never reaches Register is invisible to the
// user no matter how correct its descriptor is.
func TestRegisterRuntimes_PiAndOmpBothReachTheServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakeDir := t.TempDir()
	for _, name := range []string{"pi", "omp"} {
		writeDaemonTestExecutable(t, filepath.Join(fakeDir, name), []byte("#!/bin/sh\nexit 0\n"))
	}

	origResolve := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = origResolve })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return map[string]string{} }
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", fakeDir)
	t.Setenv("MULTICA_PI_PATH", "")
	t.Setenv("MULTICA_OMP_PATH", "")

	agents := probeAgentCLIs()
	piEntry, ok := agents["pi"]
	if !ok {
		t.Fatal("pi not discovered; the registration assertion below would be vacuous")
	}
	ompEntry, ok := agents["omp"]
	if !ok {
		t.Fatal("omp not discovered; the registration assertion below would be vacuous")
	}

	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"pi": piEntry, "omp": ompEntry}
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})

	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatalf("syncWorkspacesFromAPI: %v", err)
	}

	types, calls := fx.registrationFor("ws-1")
	if calls == 0 {
		t.Fatal("no Register call reached the server")
	}
	registered := make(map[string]bool, len(types))
	for _, ty := range types {
		registered[ty] = true
	}
	if !registered["pi"] || !registered["omp"] {
		t.Fatalf("registered runtime types = %v, want both pi and omp", types)
	}

	// The point of a separate runtime identity is that the user can tell the
	// two apart, so the payload has to carry omp's own name — not Pi's.
	if got := fx.registeredNameFor("ws-1", "omp"); got != "Oh-My-Pi" {
		t.Errorf("registered omp name = %q, want %q", got, "Oh-My-Pi")
	}
	if got := fx.registeredNameFor("ws-1", "pi"); got != "Pi" {
		t.Errorf("registered pi name = %q, want %q", got, "Pi")
	}
}
