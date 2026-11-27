package daemon

import (
	"log/slog"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// gateCodexResumeToContextSize starts a fresh Codex thread instead of resuming
// one that is already nearly full. Every run on an issue appends its whole
// prompt to the same thread, and Codex compaction keeps all of those prompts,
// so after enough runs a resumed thread cannot compact below its own trigger:
// the run compacts every few tool calls, loses its place, and loops
// (GH #11652). The fresh thread gets the same cold-start prompt and
// continuity notice as any other unavailable resume.
//
// The size is the latest request's input from the rollout's last token_count
// event. Anything the gate cannot read (no plain rollout, no token_count, no
// context window, a compaction after the last token_count) leaves the resume
// alone.
func gateCodexResumeToContextSize(task *Task, taskCtx *execenv.TaskContextForEnv, provider, codexHome string, percent int, taskLog *slog.Logger) {
	if provider != "codex" || task.PriorSessionID == "" || codexHome == "" || percent <= 0 {
		return
	}
	usage, ok := execenv.CodexResumeContextUsage(codexHome, task.PriorSessionID)
	if !ok || usage.ContextWindow <= 0 || usage.InputTokens*100 < usage.ContextWindow*int64(percent) {
		return
	}
	taskLog.Info("dropping prior codex session: its context is near the model window; starting a fresh thread",
		"session_id", task.PriorSessionID,
		"input_tokens", usage.InputTokens,
		"context_window", usage.ContextWindow,
		"limit_percent", percent)
	task.PriorSessionID = ""
	taskCtx.PriorSessionResumed = false
	taskCtx.PriorSessionResumeUnavailable = true
	task.PriorSessionResumeUnavailable = true
}
