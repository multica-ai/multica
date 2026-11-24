package daemon

import (
	"context"
	"errors"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// gateOpenCodeResumeToSaneClock returns the retired pointer before changing
// either prompt carrier. Callers must persist it even if fresh launch fails.
func gateOpenCodeResumeToSaneClock(ctx context.Context, task *Task, taskCtx *execenv.TaskContextForEnv, provider string, cfg agent.Config, cwd string, mcpConfig []byte, taskLog *slog.Logger) string {
	if provider != "opencode" || task.PriorSessionID == "" {
		return ""
	}
	var customArgs []string
	if task.Agent != nil {
		customArgs = task.Agent.CustomArgs
	}
	err := agent.CheckOpenCodeResumeClock(ctx, cfg, agent.ExecOptions{
		Cwd: cwd, ResumeSessionID: task.PriorSessionID, CustomArgs: customArgs, McpConfig: mcpConfig,
	})
	if err == nil {
		taskLog.Debug("OpenCode resume clock checked", "resume_clock_check", "verified")
		return ""
	}
	if !errors.Is(err, agent.ErrOpenCodeClockSkew) {
		taskLog.Warn("OpenCode resume clock check unavailable; continuing existing resume behavior", "resume_clock_check", "unverified", "reason", err)
		return ""
	}
	retired := task.PriorSessionID
	task.PriorSessionID = ""
	task.PriorSessionResumeUnavailable = true
	taskCtx.PriorSessionResumed = false
	taskCtx.PriorSessionResumeUnavailable = true
	taskLog.Warn("OpenCode session has future timestamps; starting a fresh session", "resume_clock_check", "unsafe", "retired_session_id", retired)
	return retired
}
