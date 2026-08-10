package daemon

import (
	"context"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// runnableTaskModel returns a model this runtime can actually spawn (FIR-4492).
//
// A task can arrive pinned to a model the runtime cannot run: a wakeup or
// workflow step carrying a model_override, or a Mode profile model, aimed at a
// runtime whose accepted model IDs no server-side catalog knows. The server
// validates what it can (autopilotmodel.ResolveForProvider, StaticCatalog), but
// for install- or account-scoped providers — hermes, opencode, cursor, kimi,
// kiro, pi, openclaw, antigravity, firtal-gateway — it has no authoritative
// list and lets the ID through. Spawning on it fails the whole run, and for a
// wakeup that run was the only thing that would have retried it.
//
// The daemon is the last place that can still tell, because it is the only one
// with live discovery. So the pin loses to the run: an unrunnable model degrades
// to the agent's own model, and then to the runtime default, which always
// spawns. Every uncertain answer keeps the pin — see agent.ModelRunnable.
//
// CEREBRO-PATCH(daemon-runnable-model-keep-acp-pin): when no better fallback
// exists, Claude/Codex can drop to "" so the CLI uses its install default. ACP
// backends (Hermes/Kimi/Kiro) MUST NOT: empty Model skips session/set_model, so
// a resumed Hermes session keeps whatever base_url was sticky in state.db
// (observed: OpenCode + Firtal deepseek id → ModelError not supported). Keep
// the original pin so Multica always re-applies custom:firtal-gateway:*.
func runnableTaskModel(ctx context.Context, provider string, entry AgentEntry, task Task, model string, taskLog *slog.Logger) string {
	if agent.ModelRunnable(ctx, provider, entry.Path, model) {
		return model
	}
	fallback := ""
	if task.Agent != nil && task.Agent.Model != "" {
		fallback = task.Agent.Model
	} else if entry.Model != "" {
		fallback = entry.Model
	}
	if fallback == model || !agent.ModelRunnable(ctx, provider, entry.Path, fallback) {
		fallback = ""
	}
	// CEREBRO-PATCH(daemon-runnable-model-keep-acp-pin): see doc above.
	if fallback == "" && model != "" && keepsPinnedModelWhenUncatalogued(provider) {
		taskLog.Warn("model: not in this runtime's catalog; keeping pin so ACP set_model still runs",
			"provider", provider,
			"model", model,
		)
		return model
	}
	taskLog.Warn("model: not in this runtime's catalog; falling back so the task still runs",
		"provider", provider,
		"model", model,
		"fallback", fallback,
	)
	return fallback
}

// keepsPinnedModelWhenUncatalogued is true for ACP backends where Multica must
// call session/set_model with the agent pin every turn.
// CEREBRO-PATCH(daemon-runnable-model-keep-acp-pin).
func keepsPinnedModelWhenUncatalogued(provider string) bool {
	switch provider {
	case "hermes", "kimi", "kiro":
		return true
	default:
		return false
	}
}

// isStickyProviderModelError detects ACP failures caused by a resumed session
// keeping the wrong provider/base_url for the Multica-pinned model.
// CEREBRO-PATCH(daemon-hermes-sticky-provider-retry).
func isStickyProviderModelError(provider, errText string) bool {
	if !keepsPinnedModelWhenUncatalogued(provider) || errText == "" {
		return false
	}
	lower := strings.ToLower(errText)
	if strings.Contains(lower, "modelerror") {
		return true
	}
	if strings.Contains(lower, "is not supported") {
		return true
	}
	// OpenCode rejecting a Firtal/TensorX model id after sticky base_url reuse.
	if strings.Contains(lower, "opencode") && strings.Contains(lower, "not supported") {
		return true
	}
	return false
}
