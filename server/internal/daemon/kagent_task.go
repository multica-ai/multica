package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/kagent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func validateKagentTask(task Task) error {
	if err := validateTaskIdentity(task); err != nil {
		return err
	}
	if task.Agent == nil || task.WorkspaceID == "" || task.ID == "" || task.RuntimeID == "" || !task.StartClaimSupported || task.DispatchedAt == "" || task.ChatSessionID == "" || strings.TrimSpace(task.ChatMessage) == "" {
		return fmt.Errorf("kagent: authoritative agent/workspace/chat/claim identities and text required")
	}
	if err := agent.ValidateKagentRuntimeConfig(task.Agent.RuntimeConfig); err != nil {
		return err
	}
	a := task.Agent
	if task.AuthToken != "" || task.RemoteMCPDaemonToken != "" || task.IssueID != "" || task.AutopilotID != "" || task.AutopilotRunID != "" || task.WakeupID != "" || task.QuickCreatePrompt != "" || task.RegenerateQuickActionsFor != "" || task.ChatIntro ||
		len(task.Repos) > 0 || len(task.ProjectResources) > 0 || len(task.ChatMessageAttachments) > 0 || len(task.ConnectedApps) > 0 || len(task.RemoteMCPConnections) > 0 || len(task.PluginHookTools) > 0 ||
		len(a.Skills) > 0 || len(a.SkillRefs) > 0 || len(a.DisabledRuntimeSkills) > 0 || len(a.CustomArgs) > 0 || len(a.CustomEnv) > 0 || !emptyKagentJSON(a.McpConfig) ||
		a.Model != "" || a.ThinkingLevel != "" || a.ServiceTier != "" || task.ModelOverride != "" || task.PriorSessionResumeUnavailable {
		return fmt.Errorf("kagent: native-text chat does not support credentials, workspace/tools, attachments, model overrides or other task kinds")
	}
	return nil
}

func emptyKagentJSON(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "{}"
}

func kagentTaskBrief(task Task) string {
	return fmt.Sprintf("Native text DEV execution. Answer the supplied chat message in text. No checkout, filesystem, Multica CLI, task credentials, skills, MCP or connected apps are available. Do not claim to perform actions through those capabilities.\n\nAgent: %s\nWorkspace: %s\nConversation: %s\n\nAgent instructions:\n%s\n\nWorkspace context:\n%s\n\nProject context:\n%s\n%s",
		task.Agent.Name, task.WorkspaceID, task.ChatSessionID, task.Agent.Instructions, task.WorkspaceContext, task.ProjectTitle, task.ProjectDescription)
}

func (d *Daemon) runKagentTask(ctx context.Context, task Task, taskLog *slog.Logger) (TaskResult, error) {
	// All unsupported inputs are rejected before provisioning or StartTask.
	if err := validateKagentTask(task); err != nil {
		return TaskResult{}, err
	}
	cfg, err := loadKagentDaemonConfig()
	if err != nil {
		return TaskResult{}, err
	}
	remote, err := cfg.backendConfig(task)
	if err != nil {
		return TaskResult{}, err
	}
	backend, err := agent.ResolveBackend("kagent", agent.Config{Kagent: remote})
	if err != nil {
		return TaskResult{}, err
	}
	if _, err := d.client.StartTask(ctx, task, protocol.DaemonCapabilityKagentProcessCleanupV1); err != nil {
		return TaskResult{}, fmt.Errorf("start task failed: %w", err)
	}
	var seq atomic.Int32
	result, _, err := d.executeAndDrain(ctx, backend, task.ChatMessage, agent.ExecOptions{
		SystemPrompt: kagentTaskBrief(task), Timeout: d.cfg.AgentTimeout, MulticaWorkspaceID: task.WorkspaceID,
		ResumeSessionID: task.PriorSessionID, ResumeExpected: task.PriorSessionID != "",
	}, taskLog, task.ID, "", &seq)
	if errors.Is(err, kagent.ErrUnknownOutcome) {
		d.cleanupUnconfirmed.Store(true)
		return TaskResult{Status: "failed", FailureReason: "cleanup_unconfirmed", Comment: err.Error()}, nil
	}
	if err != nil {
		return TaskResult{}, err
	}
	out := kagentTaskResult(result)
	return out, nil
}

// Match the existing daemon timeout disposition: a confirmed remote cancel is
// safe to report as timeout, not as deliberate user cancellation or generic failure.
func kagentTaskResult(result agent.Result) TaskResult {
	out := TaskResult{Status: result.Status, Comment: result.Output, SessionID: result.SessionID}
	if result.Status == "cleanup_unconfirmed" {
		out.Status, out.FailureReason, out.Comment = "failed", "cleanup_unconfirmed", result.Error
	} else if result.Status == "timeout" {
		out.Status, out.FailureReason, out.Comment = "blocked", "timeout", result.Error
	} else if result.Error != "" {
		out.Comment = result.Error
	}
	return out
}
