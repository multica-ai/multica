package daemon

import (
	"encoding/json"
	"fmt"
	"os"

	piharness "github.com/multica-ai/multica/packages/cerebro-pi-harness"
)

// piHarnessFeatureKey is the workspace kill switch that decides whether the
// harness is installed. Named here so the refusal message points the operator
// at the exact flag to turn back on.
const piHarnessFeatureKey = "cerebro_pi_harness"

// preparePiHarness gives Pi exactly one Firtal-owned extension. The extension
// projects Multica MCP tools and applies the same claim-time policy stage as
// Claude's PreToolUse hook. Other providers remain byte-for-byte unchanged.
func preparePiHarness(enabled bool, provider, workdir, policyStage string, customArgs []string, mcpConfig json.RawMessage, agentEnv map[string]string) ([]string, error) {
	if provider != "pi" {
		return customArgs, nil
	}
	// The harness IS Pi's tool-policy adapter: it owns the pi.on("tool_call")
	// gate that every Pi tool call must pass. Running Pi with the harness
	// switched off would run it with no call-time enforcement at all, so the
	// kill switch has to stop the spawn rather than silently ungate it.
	if !enabled {
		return nil, fmt.Errorf("local provider %q requires the Pi harness for mandatory tool-policy enforcement, but %s is disabled for this workspace", provider, piHarnessFeatureKey)
	}
	path, err := piharness.Prepare(workdir)
	if err != nil {
		return nil, err
	}
	args, err := piharness.ManagedArgs(customArgs, path)
	if err != nil {
		return nil, err
	}
	connectionsPath, err := piharness.PrepareConnections(workdir, mcpConfig)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	agentEnv["MULTICA_PI_HARNESS_COMMAND"] = executable
	agentEnv["MULTICA_PI_HARNESS_MCP_CONFIG"] = connectionsPath
	agentEnv["CEREBRO_TOOLPOLICY_STAGE"] = policyStage
	return args, nil
}
