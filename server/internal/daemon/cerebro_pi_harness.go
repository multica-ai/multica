package daemon

import (
	"encoding/json"
	"os"

	piharness "github.com/multica-ai/multica/packages/cerebro-pi-harness"
)

// preparePiHarness gives Pi exactly one Firtal-owned extension. The extension
// projects Multica MCP tools and applies the same claim-time policy stage as
// Claude's PreToolUse hook. Other providers remain byte-for-byte unchanged.
func preparePiHarness(enabled bool, provider, workdir, policyStage string, customArgs []string, mcpConfig json.RawMessage, agentEnv map[string]string) ([]string, error) {
	if !enabled || provider != "pi" {
		return customArgs, nil
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
