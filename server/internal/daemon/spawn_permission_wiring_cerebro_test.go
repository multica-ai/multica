package daemon

// CEREBRO-PATCH(daemon-spawn-permission-wiring-guard): net-new cerebro tripwire
// test (FIR-2743).
//
// Upstream sync #4530 replaced daemon/daemon.go and silently deleted the marked
// daemon-side halves of the local tool-policy hook (TECH-2563), the sandbox
// spawn wrapper (JEH-418/FIR-1428) and the user communication profile injection
// (JEH-304) — leaving the server-side claim fields dead on arrival. This
// source-presence tripwire fails loudly in CI the next time the spawn wiring
// disappears. Same pattern as usage_footprint_wiring_cerebro_test.go.

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonSpawnPermissionWiringPresent(t *testing.T) {
	t.Parallel()

	const path = "daemon.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := strings.Join(strings.Fields(string(src)), " ")

	required := []struct {
		fragment string
		why      string
	}{
		{
			fragment: "d.prepareToolPolicySpawn(provider, task.LocalToolPolicyStage, env.WorkDir)",
			why:      "runTask must wire the Claude PreToolUse hook from the claim's stage, or local per-tool enforcement never reaches the CLI (TECH-2563).",
		},
		{
			fragment: `customArgs = append(customArgs, "--settings", toolPolicySpawn.SettingsPath)`,
			why:      "the PreToolUse hook settings file must be passed to Claude Code via --settings, or the hook is written but never loaded (TECH-2563).",
		},
		{
			fragment: "Sandbox: d.buildSandboxConfig(provider, task.SandboxEnabled, parseRuntimeSandboxPolicy(task.RuntimeSandboxPolicy), task.Agent)",
			why:      "agent.New must receive the sandbox config, or agents spawn unsandboxed and the runtime sandbox override + agent-browser gate are dead (JEH-418/FIR-1428).",
		},
		{
			fragment: "UserProfilePrompt: task.UserProfilePrompt",
			why:      "the execenv task context must carry the compiled user profile, or the brief never renders the User Communication Profile section (JEH-304).",
		},
	}

	for _, r := range required {
		if !strings.Contains(body, r.fragment) {
			t.Errorf("daemon.go is missing required cerebro spawn wiring:\n  fragment: %q\n  why: %s\n  (likely deleted by an upstream sync — see FIR-2743 / docs/cerebro-patches.md)", r.fragment, r.why)
		}
	}
}
