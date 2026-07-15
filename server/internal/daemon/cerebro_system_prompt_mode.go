package daemon

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// decodeSystemPromptMode reads the agent's configured system-prompt delivery
// mode out of its runtime_config JSONB (FIR-3212, slice 3).
//
// This is the missing half of slice 1: agent.ExecOptions.SystemPromptMode and
// claude.go's handling of it landed together, but nothing ever set the field, so
// every run took the empty default and the setting was unreachable. This is the
// read side of the value agentoffice.WithSystemPromptMode writes.
//
// It mirrors decodeOpenclawRuntimeConfig, the other per-agent runtime_config
// knob that feeds ExecOptions, and delegates to agentoffice so the key name and
// the accepted vocabulary have exactly one definition. An absent, malformed, or
// unrecognised value yields the default, which preserves each backend's
// pre-FIR-3212 behaviour — an agent is never silently switched to a mode it did
// not ask for.
func decodeSystemPromptMode(raw json.RawMessage) agent.SystemPromptMode {
	return agent.SystemPromptMode(
		agentoffice.SystemPromptModeOf(agentoffice.ContextSnapshot{RuntimeConfig: raw}),
	)
}
