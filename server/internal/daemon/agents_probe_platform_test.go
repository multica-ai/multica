package daemon

import "testing"

// TestProbeAgentCLIsPlatformAgentPinnedPath keeps platform-agent-cli
// discovery deterministic and ensures an explicit operator path is used as
// both the resolved executable and launch command.
func TestProbeAgentCLIsPlatformAgentPinnedPath(t *testing.T) {
	orig := resolveAgentsViaLoginShell
	t.Cleanup(func() { resolveAgentsViaLoginShell = orig })
	resolveAgentsViaLoginShell = func([]string) map[string]string {
		return map[string]string{}
	}
	resetShellResolveCacheForTest(t)

	path := fakeExecutable(t, "platform-agent-cli")
	t.Setenv("PATH", "")
	t.Setenv("MULTICA_PLATFORM_AGENT_CLI_PATH", path)

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
