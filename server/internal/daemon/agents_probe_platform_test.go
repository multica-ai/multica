package daemon

import (
	"path/filepath"
	"testing"
)

// TestProbeAgentCLIsDesktopBundledPath ensures Desktop's bundled-only mode
// launches exactly the absolute executable path injected by Desktop.
func TestProbeAgentCLIsDesktopBundledPath(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{}
	}
	resetShellResolveCacheForTest(t)

	path := fakeExecutable(t, "platform-agent-cli")
	t.Setenv("PATH", "")
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_PATH", path)
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY", "1")

	agents := probeAgentCLIs()
	entry, ok := agents["platform-agent-cli"]
	if !ok {
		t.Fatal("platform-agent-cli was not discovered")
	}
	if entry.Path != path || entry.Command != path || entry.Model != "" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

// TestPlatformAgentDefaultCommandName keeps the login-shell fallback aware of
// the platform CLI when it is installed outside the daemon's inherited PATH.
func TestPlatformAgentDefaultCommandName(t *testing.T) {
	for _, name := range defaultAgentCommandNames {
		if name == "platform-agent-cli" {
			return
		}
	}
	t.Fatal("defaultAgentCommandNames is missing platform-agent-cli")
}

func TestProbeAgentCLIsDesktopBundledOnlyDoesNotFallBack(t *testing.T) {
	fakePath := fakeExecutable(t, "platform-agent-cli")

	for _, tc := range []struct {
		name          string
		path          string
		shellResolved map[string]string
	}{
		{
			name: "inherited PATH",
			path: filepath.Dir(fakePath),
		},
		{
			name:          "login shell",
			path:          "",
			shellResolved: map[string]string{"platform-agent-cli": fakePath},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := resolveAgentsViaLoginShell
			t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
			resolveAgentsViaLoginShell = func([]string) map[string]string {
				return tc.shellResolved
			}
			resetShellResolveCacheForTest(t)

			t.Setenv("PATH", tc.path)
			t.Setenv("MULTICA_PLATFORM_AGENT_CLI_PATH", "")
			t.Setenv("MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY", "1")

			if entry, ok := probeAgentCLIs()["platform-agent-cli"]; ok {
				t.Fatalf("Desktop bundled-only probe discovered fallback CLI: %+v", entry)
			}
		})
	}
}

func TestProbeAgentCLIsStandaloneStillDiscoversPlatformAgentOnPath(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string { return nil }
	resetShellResolveCacheForTest(t)

	path := fakeExecutable(t, "platform-agent-cli")
	t.Setenv("PATH", filepath.Dir(path))
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_PATH", "")
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY", "")

	entry, ok := probeAgentCLIs()["platform-agent-cli"]
	if !ok {
		t.Fatal("standalone daemon did not discover platform-agent-cli on PATH")
	}
	if entry.Path == "" || entry.Command != "platform-agent-cli" {
		t.Fatalf("unexpected standalone entry: %+v", entry)
	}
}
