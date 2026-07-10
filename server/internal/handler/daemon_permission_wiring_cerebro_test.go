package handler

// CEREBRO-PATCH(daemon-permission-wiring-guard): net-new cerebro tripwire test (FIR-2743).
//
// Upstream sync #4454 replaced handler/daemon.go and silently deleted the marked
// cerebro wirings for staged local tool-policy (TECH-2563), claim-time capability
// policy (FIR-458), the capability-register mirrors (FIR-2129/FIR-2284), the
// per-runtime sandbox override (JEH-418) and the user communication profile
// (JEH-304). A CEREBRO-PATCH marker cannot catch a deletion — once the line is
// gone there is no marker left to validate. This source-presence tripwire fails
// loudly in CI the next time any of these wirings disappears. Same pattern as
// daemon_usage_wiring_cerebro_test.go (FIR-2189).

import (
	"os"
	"strings"
	"testing"
)

func TestDaemonPermissionCerebroWiringPresent(t *testing.T) {
	t.Parallel()

	const path = "daemon.go"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Collapse all whitespace runs to single spaces so the check is immune to
	// gofmt re-alignment.
	body := strings.Join(strings.Fields(string(src)), " ")

	required := []struct {
		fragment string
		why      string
	}{
		{
			fragment: "resp.LocalToolPolicyStage = string(stage)",
			why:      "ClaimTaskByRuntime must return the staged local tool-policy mode, or the daemon never wires the Claude PreToolUse hook and local per-tool enforcement is silently off (TECH-2563).",
		},
		{
			fragment: "applyCapabilityPolicyToClaim(&resp, ws.Settings, runtimeID)",
			why:      "ClaimTaskByRuntime must apply the workspace capability policy, or sandbox allowlists and per-agent MCP denies from Settings are silently unenforced (FIR-458).",
		},
		{
			fragment: "h.persistRuntimeCapabilitySnapshot(r, registered.ID, registered.WorkspaceID, raw)",
			why:      "DaemonRegister must mirror the initial capability snapshot into the register, or reconnecting runtimes never surface their tools in the unified tool table (FIR-2129).",
		},
		{
			fragment: "h.persistRuntimeCapabilitySnapshot(r, rt.ID, rt.WorkspaceID, raw)",
			why:      "DaemonHeartbeat must mirror capability drift reports into the register, or CLI upgrades leave the tool table stale (FIR-2129).",
		},
		{
			fragment: "h.maybeMirrorRuntimeCapabilitySnapshot(rt.ID, rt.WorkspaceID, rt.Capabilities)",
			why:      "processHeartbeat must re-mirror the runtime snapshot (throttled), or runtimes that never change CLI version never land their tools in the register (FIR-2284).",
		},
		{
			fragment: "resp.SandboxEnabled = boolToPtr(runtime.SandboxEnabled)",
			why:      "ClaimTaskByRuntime must surface the per-runtime sandbox override, or the daemon spawns agents with the env-var default regardless of admin settings (JEH-418).",
		},
		{
			fragment: "resp.RuntimeSandboxPolicy = json.RawMessage(runtime.SandboxPolicy)",
			why:      "ClaimTaskByRuntime must surface the runtime sandbox policy, or the agent-browser sandbox gate (FIR-1428) folds its grant into an empty policy the daemon never receives.",
		},
		{
			fragment: "resp.UserProfilePrompt = h.compileProfileForUser(r.Context(), comment.AuthorID)",
			why:      "Comment-triggered claims must carry the member author's communication profile, or user profile settings are silently ignored (JEH-304).",
		},
		{
			fragment: "resp.UserProfilePrompt = h.compileProfileForUser(r.Context(), cs.CreatorID)",
			why:      "Chat claims must carry the session creator's communication profile, or user profile settings are silently ignored in chat (JEH-304).",
		},
	}

	for _, r := range required {
		if !strings.Contains(body, r.fragment) {
			t.Errorf("daemon.go is missing required cerebro permission wiring:\n  fragment: %q\n  why: %s\n  (likely deleted by an upstream sync — see FIR-2743 / docs/cerebro-patches.md)", r.fragment, r.why)
		}
	}
}
