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

// systemPromptModeForTask reads the mode for a task that may carry no agent data.
//
// The claim path only attaches Agent when GetAgent succeeds (handler/daemon.go),
// so a transient lookup failure dispatches the task with Agent nil. runTask
// guards every other agent-derived field the same way; without this the mode
// read would panic the daemon process rather than fail the one task.
func systemPromptModeForTask(task Task) agent.SystemPromptMode {
	if task.Agent == nil {
		return agent.SystemPromptModeDefault
	}
	return decodeSystemPromptMode(task.Agent.RuntimeConfig)
}

// needsInlineRuntimeBrief reports whether the runtime brief must be handed to
// the backend inline (ExecOptions.SystemPrompt) instead of being left to the
// config file the task workdir already carries (FIR-3212, slice 3 follow-up).
//
// Two independent reasons, either one sufficient:
//
//   - The provider does not reliably load the workdir config file at all —
//     providerNeedsInlineSystemPrompt, the pre-FIR-3212 rule, unchanged.
//   - The agent explicitly configured a system_prompt_mode this provider can
//     honour. Slice 1 taught the backends to deliver a system prompt in the
//     requested mode and slice 3 taught the daemon to read the mode, but the
//     prompt itself was still only filled for the five file-unreliable
//     providers. On every other backend ClaudeSystemPromptArgs and its peers
//     therefore received an empty prompt and emitted no flag at all: the mode
//     was stored, validated, versioned, diffed and rendered in the UI, and
//     changed nothing at run time. That is the exact dishonesty FIR-3212 exists
//     to remove, and it landed on claude — the provider the issue was raised
//     about ("agenter som ikke har den fulde Claude code instruction").
//
// Agents that never chose a mode are untouched: SystemPromptModeDefault
// short-circuits before the support lookup, so their runs keep byte-identical
// behaviour. That is what makes this safe to ship to a live fleet.
func needsInlineRuntimeBrief(provider string, mode agent.SystemPromptMode) bool {
	return providerNeedsInlineSystemPrompt(provider) || modeNeedsInlineRuntimeBrief(provider, mode)
}

// modeNeedsInlineRuntimeBrief reports whether an explicitly configured mode can
// only take effect if the brief travels inline.
//
// An unknown provider answers false, not true: SystemPromptSupportFor's
// contract is that ok=false means "no authoritative entry", never "supports
// nothing". Injecting the full brief into a backend we have not catalogued
// would rewrite that backend's prompt on the strength of a guess — the opposite
// of the StaticCatalog contract the rest of this feature is built on.
//
// A provider that accepts the mode but discards the prompt anyway (hermes) is
// not special-cased here: it is already covered by the first clause, and its
// support entry lists no modes, so this predicate answers false for it.
func modeNeedsInlineRuntimeBrief(provider string, mode agent.SystemPromptMode) bool {
	if mode == agent.SystemPromptModeDefault {
		return false
	}
	support, ok := agent.SystemPromptSupportFor(provider)
	if !ok {
		return false
	}
	return support.Supports(mode)
}
