package daemon

import (
	"testing"
)

// TestProbeAgentCLIs_QoderResolvesViaLoginShell locks down the fix for the
// Qoder discovery gap reported from the desktop app (MUL-5524).
//
// Every other provider is probed through the shared probe() helper, which falls
// back to the user's login shell when the daemon's own PATH can't see a bare
// command name. Qoder was probed with a bare resolveAgentExecutablePath call
// and therefore had no fallback at all: a GUI/Launchpad-started daemon (the
// apple.dmg desktop build) does not inherit the interactive shell PATH, so a
// `qodercli` living in an npm global prefix or any ~/.zshrc-added dir was
// undetectable no matter how many times the daemon restarted.
func TestProbeAgentCLIs_QoderResolvesViaLoginShell(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{"qodercli": "/fake/npm-global/bin/qodercli"}
	}
	resetShellResolveCacheForTest(t)

	// An empty PATH guarantees the exec.LookPath leg misses, so the only way
	// qoder can resolve is the login-shell fallback.
	t.Setenv("PATH", "")

	agents := probeAgentCLIs()
	entry, ok := agents["qoder"]
	if !ok {
		t.Fatal("qoder was not discovered via the login-shell fallback; " +
			"a GUI-launched daemon cannot see a qodercli installed on the interactive shell PATH")
	}
	if entry.Path != "/fake/npm-global/bin/qodercli" {
		t.Errorf("qoder path = %q, want /fake/npm-global/bin/qodercli", entry.Path)
	}
	if entry.Command != "qodercli" {
		t.Errorf("qoder command = %q, want qodercli", entry.Command)
	}
}

func TestProbeAgentCLIs_QoderCNResolvesIndependently(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{
			"qodercli":   "/fake/bin/qodercli",
			"qoderclicn": "/fake/bin/qoderclicn",
		}
	}
	resetShellResolveCacheForTest(t)
	t.Setenv("PATH", "")

	agents := probeAgentCLIs()
	entry, ok := agents["qoderclicn"]
	if !ok {
		t.Fatal("qoderclicn was not discovered via the login-shell fallback")
	}
	if entry.Path != "/fake/bin/qoderclicn" {
		t.Errorf("qoderclicn path = %q, want /fake/bin/qoderclicn", entry.Path)
	}
	if entry.Command != "qoderclicn" {
		t.Errorf("qoderclicn command = %q, want qoderclicn", entry.Command)
	}
	if _, ok := agents["qoder"]; !ok {
		t.Fatal("qodercli and qoderclicn must be registered independently when both are installed")
	}
}

// TestProbeAgentCLIs_QoderPinnedPathStaysHardMiss keeps the fallback from
// rescuing an operator-pinned MULTICA_QODER_PATH. A pinned absolute path that
// no longer exists must stay a miss rather than silently resolve a different
// binary — the same rule probe() applies to every other provider.
func TestProbeAgentCLIs_QoderPinnedPathStaysHardMiss(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{"qodercli": "/fake/npm-global/bin/qodercli"}
	}
	resetShellResolveCacheForTest(t)

	t.Setenv("PATH", "")
	t.Setenv("MULTICA_QODER_PATH", "/nonexistent/pinned/qodercli")

	if entry, ok := probeAgentCLIs()["qoder"]; ok {
		t.Errorf("pinned-but-missing MULTICA_QODER_PATH resolved to %q, want a hard miss", entry.Path)
	}
}
