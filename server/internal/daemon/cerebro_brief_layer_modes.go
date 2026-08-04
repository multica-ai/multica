package daemon

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
)

// Brief-layer mode decoding (FIR-3212) — the read side of the values
// agentoffice.WithWorkspaceBriefMode / WithToolsBriefMode write.
//
// Mirrors decodeSystemPromptMode: the key names and accepted vocabulary have
// exactly one definition, in agentoffice. An absent, malformed, or unrecognised
// value yields the default, which preserves today's brief for every agent that
// never chose a mode — an agent is never silently switched to a thinner brief
// it did not ask for.

func decodeWorkspaceBriefMode(raw json.RawMessage) string {
	return agentoffice.WorkspaceBriefModeOf(agentoffice.ContextSnapshot{RuntimeConfig: raw})
}

func decodeToolsBriefMode(raw json.RawMessage) string {
	return agentoffice.ToolsBriefModeOf(agentoffice.ContextSnapshot{RuntimeConfig: raw})
}

// briefLayerModesForTask reads both modes for a task that may carry no agent
// data. The claim path only attaches Agent when GetAgent succeeds
// (handler/daemon.go), so a transient lookup failure dispatches the task with
// Agent nil; without this guard the reads would panic the daemon process
// rather than fail the one task.
//
// The workspace-level cerebro_tools_brief flag (FIR-4500) is resolved
// server-side and arrives as Task.ToolsBriefDisabled. It wins over the agent's
// own tools_brief_mode, and it applies even when Agent is nil: the workspace
// said "do not ship this section", which cannot depend on an agent lookup
// having succeeded.
func briefLayerModesForTask(task Task) (workspaceBriefMode, toolsBriefMode string) {
	if task.ToolsBriefDisabled {
		if task.Agent == nil {
			return "", agentoffice.ToolsBriefModeOff
		}
		return decodeWorkspaceBriefMode(task.Agent.RuntimeConfig), agentoffice.ToolsBriefModeOff
	}
	if task.Agent == nil {
		return "", ""
	}
	return decodeWorkspaceBriefMode(task.Agent.RuntimeConfig), decodeToolsBriefMode(task.Agent.RuntimeConfig)
}
