package daemon

import (
	"context"
	"log/slog"

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
	taskLog.Warn("model: not in this runtime's catalog; falling back so the task still runs",
		"provider", provider,
		"model", model,
		"fallback", fallback,
	)
	return fallback
}
