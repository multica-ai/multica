package daemon

import (
	"encoding/json"
	"log/slog"
)

// taskComputerMCPName is the MCP server name task agents see for Computer Use.
const taskComputerMCPName = "multica_computer"

// taskComputerMCP binds Computer Use for one task when this daemon runs inside
// Multica Desktop, Desktop is connected with a compatible Computer capability
// and signed in as this daemon's account, and the provider accepts a
// task-local HTTP MCP server. It returns the MCP config fragment to merge and
// Desktop's prompt brief, or nothing; failures never fail the task.
func (d *Daemon) taskComputerMCP(task Task, provider string, logger *slog.Logger) (json.RawMessage, string, func()) {
	if d.conversations == nil || task.Agent == nil || !providerSupportsRemoteMCPBroker(provider) {
		return nil, "", nil
	}
	bound, release, ok, err := d.conversations.BindTaskComputer(task.ID, task.WorkspaceID, d.accountUserID())
	if err != nil {
		logger.Warn("Computer Use MCP unavailable", "error", err)
		return nil, "", nil
	}
	if !ok {
		return nil, "", nil
	}
	raw, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		taskComputerMCPName: map[string]any{"type": "http", "url": bound.URL},
	}})
	if err != nil {
		release()
		return nil, "", nil
	}
	return raw, bound.Brief, release
}
