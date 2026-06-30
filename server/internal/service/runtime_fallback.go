package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

const RuntimeRestoreContextType = "runtime_restore"

// RuntimeRestoreContext is stored on deferred agent_task_queue rows whose sole
// job is to switch an agent back to its preferred runtime after a rate-limit
// cooldown. PromoteDueDeferredTasksForRuntime applies these inline — they never
// enter the daemon claim path.
type RuntimeRestoreContext struct {
	Type                 string `json:"type"`
	PreferredRuntimeID   string `json:"preferred_runtime_id"`
	FallbackRuntimeID    string `json:"fallback_runtime_id,omitempty"`
	TriggeringTaskID     string `json:"triggering_task_id,omitempty"`
}

// RuntimeFallbackConfig is read from agent.runtime_config JSONB.
type RuntimeFallbackConfig struct {
	FallbackRuntimeID  string `json:"fallback_runtime_id"`
	PreferredRuntimeID string `json:"preferred_runtime_id"`
}

func parseRuntimeFallbackConfig(raw []byte) RuntimeFallbackConfig {
	if len(raw) == 0 {
		return RuntimeFallbackConfig{}
	}
	var cfg RuntimeFallbackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return RuntimeFallbackConfig{}
	}
	return cfg
}

func parseRuntimeRestoreContext(raw []byte) (RuntimeRestoreContext, bool) {
	if len(raw) == 0 {
		return RuntimeRestoreContext{}, false
	}
	var payload RuntimeRestoreContext
	if err := json.Unmarshal(raw, &payload); err != nil {
		return RuntimeRestoreContext{}, false
	}
	if payload.Type != RuntimeRestoreContextType || payload.PreferredRuntimeID == "" {
		return RuntimeRestoreContext{}, false
	}
	return payload, true
}

func mergeRuntimeFallbackConfig(raw []byte, patch RuntimeFallbackConfig) []byte {
	cfg := parseRuntimeFallbackConfig(raw)
	if patch.FallbackRuntimeID != "" {
		cfg.FallbackRuntimeID = patch.FallbackRuntimeID
	}
	if patch.PreferredRuntimeID != "" {
		cfg.PreferredRuntimeID = patch.PreferredRuntimeID
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return raw
	}
	return out
}

// maybeApplyRuntimeRateLimitFallback switches the agent (and its retry child) to
// a configured fallback runtime when the parent task failed with a provider
// capacity / rate-limit reason, and schedules a deferred restore on the
// preferred runtime when the cooldown elapses.
func (s *TaskService) maybeApplyRuntimeRateLimitFallback(ctx context.Context, parent db.AgentTaskQueue, child *db.AgentTaskQueue) {
	if child == nil {
		return
	}
	reason := ""
	if parent.FailureReason.Valid {
		reason = parent.FailureReason.String
	}
	if reason != taskfailure.ReasonAgentProviderCapacityOrRateLimit.String() {
		return
	}

	agent, err := s.Queries.GetAgent(ctx, parent.AgentID)
	if err != nil {
		slog.Warn("runtime fallback skipped: agent lookup failed",
			"agent_id", util.UUIDToString(parent.AgentID),
			"task_id", util.UUIDToString(parent.ID),
			"error", err,
		)
		return
	}
	if agent.ArchivedAt.Valid {
		return
	}

	cfg := parseRuntimeFallbackConfig(agent.RuntimeConfig)
	fallbackID, err := util.ParseUUID(cfg.FallbackRuntimeID)
	if err != nil || !fallbackID.Valid {
		return
	}

	preferredID := parent.RuntimeID
	if cfg.PreferredRuntimeID != "" {
		if parsed, parseErr := util.ParseUUID(cfg.PreferredRuntimeID); parseErr == nil && parsed.Valid {
			preferredID = parsed
		}
	}
	if !preferredID.Valid || preferredID == fallbackID {
		return
	}
	// Only switch away from the preferred runtime that rate-limited.
	if agent.RuntimeID != preferredID {
		return
	}

	restoreAt := retryNotBefore(parent, time.Now())
	if !restoreAt.Valid {
		restoreAt = pgtype.Timestamptz{Time: time.Now().UTC().Add(2 * time.Minute), Valid: true}
	}

	updatedConfig := mergeRuntimeFallbackConfig(agent.RuntimeConfig, RuntimeFallbackConfig{
		FallbackRuntimeID:  util.UUIDToString(fallbackID),
		PreferredRuntimeID: util.UUIDToString(preferredID),
	})
	if _, err := s.Queries.UpdateAgent(ctx, db.UpdateAgentParams{
		ID:            agent.ID,
		RuntimeID:     fallbackID,
		RuntimeConfig: updatedConfig,
	}); err != nil {
		slog.Warn("runtime fallback: failed to switch agent runtime",
			"agent_id", util.UUIDToString(agent.ID),
			"preferred_runtime_id", util.UUIDToString(preferredID),
			"fallback_runtime_id", util.UUIDToString(fallbackID),
			"error", err,
		)
		return
	}

	if err := s.Queries.UpdateAgentTaskRuntime(ctx, db.UpdateAgentTaskRuntimeParams{
		ID:        child.ID,
		RuntimeID: fallbackID,
	}); err != nil {
		slog.Warn("runtime fallback: failed to retarget retry task runtime",
			"child_task_id", util.UUIDToString(child.ID),
			"fallback_runtime_id", util.UUIDToString(fallbackID),
			"error", err,
		)
	}

	if _, err := s.Queries.CancelDeferredRuntimeRestoreForAgent(ctx, agent.ID); err != nil {
		slog.Warn("runtime fallback: failed to cancel prior restore tasks",
			"agent_id", util.UUIDToString(agent.ID),
			"error", err,
		)
	}

	payload, err := json.Marshal(RuntimeRestoreContext{
		Type:               RuntimeRestoreContextType,
		PreferredRuntimeID: util.UUIDToString(preferredID),
		FallbackRuntimeID:  util.UUIDToString(fallbackID),
		TriggeringTaskID:   util.UUIDToString(parent.ID),
	})
	if err != nil {
		return
	}

	restoreTask, err := s.Queries.CreateDeferredRuntimeRestoreTask(ctx, db.CreateDeferredRuntimeRestoreTaskParams{
		AgentID:   agent.ID,
		RuntimeID: preferredID,
		IssueID:   parent.IssueID,
		Context:   payload,
		FireAt:    restoreAt,
	})
	if err != nil {
		slog.Warn("runtime fallback: failed to schedule deferred restore",
			"agent_id", util.UUIDToString(agent.ID),
			"preferred_runtime_id", util.UUIDToString(preferredID),
			"fire_at", restoreAt.Time.UTC().Format(time.RFC3339),
			"error", err,
		)
		return
	}

	slog.Info("runtime fallback applied",
		"agent_id", util.UUIDToString(agent.ID),
		"preferred_runtime_id", util.UUIDToString(preferredID),
		"fallback_runtime_id", util.UUIDToString(fallbackID),
		"retry_task_id", util.UUIDToString(child.ID),
		"restore_task_id", util.UUIDToString(restoreTask.ID),
		"restore_at", restoreAt.Time.UTC().Format(time.RFC3339),
	)
}

func (s *TaskService) promoteDueRuntimeRestoreTasks(ctx context.Context, runtimeID pgtype.UUID) error {
	tasks, err := s.Queries.ListDueDeferredRuntimeRestoreTasks(ctx, runtimeID)
	if err != nil {
		return fmt.Errorf("list due runtime restore tasks: %w", err)
	}
	for _, task := range tasks {
		if err := s.applyRuntimeRestore(ctx, task); err != nil {
			slog.Warn("runtime restore failed",
				"task_id", util.UUIDToString(task.ID),
				"runtime_id", util.UUIDToString(runtimeID),
				"error", err,
			)
		}
	}
	return nil
}

func (s *TaskService) applyRuntimeRestore(ctx context.Context, task db.AgentTaskQueue) error {
	payload, ok := parseRuntimeRestoreContext(task.Context)
	if !ok {
		return fmt.Errorf("invalid runtime_restore context")
	}
	preferredID, err := util.ParseUUID(payload.PreferredRuntimeID)
	if err != nil || !preferredID.Valid {
		return fmt.Errorf("invalid preferred_runtime_id")
	}

	agent, err := s.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		_, _ = s.Queries.CompleteRuntimeRestoreTask(ctx, task.ID)
		return nil
	}

	cfg := parseRuntimeFallbackConfig(agent.RuntimeConfig)
	fallbackID := agent.RuntimeID
	if cfg.FallbackRuntimeID != "" {
		if parsed, parseErr := util.ParseUUID(cfg.FallbackRuntimeID); parseErr == nil && parsed.Valid {
			fallbackID = parsed
		}
	}
	if payload.FallbackRuntimeID != "" {
		if parsed, parseErr := util.ParseUUID(payload.FallbackRuntimeID); parseErr == nil && parsed.Valid {
			fallbackID = parsed
		}
	}

	// Only restore when the agent is still on the fallback runtime from this
	// rate-limit episode. Manual runtime changes win.
	if agent.RuntimeID != fallbackID {
		_, _ = s.Queries.CompleteRuntimeRestoreTask(ctx, task.ID)
		return nil
	}

	updatedConfig := mergeRuntimeFallbackConfig(agent.RuntimeConfig, RuntimeFallbackConfig{
		PreferredRuntimeID: util.UUIDToString(preferredID),
		FallbackRuntimeID:  util.UUIDToString(fallbackID),
	})
	if _, err := s.Queries.UpdateAgent(ctx, db.UpdateAgentParams{
		ID:            agent.ID,
		RuntimeID:     preferredID,
		RuntimeConfig: updatedConfig,
	}); err != nil {
		return fmt.Errorf("restore agent runtime: %w", err)
	}

	if _, err := s.Queries.CompleteRuntimeRestoreTask(ctx, task.ID); err != nil {
		return fmt.Errorf("complete restore task: %w", err)
	}

	slog.Info("runtime restored to preferred runtime",
		"agent_id", util.UUIDToString(agent.ID),
		"preferred_runtime_id", util.UUIDToString(preferredID),
		"restore_task_id", util.UUIDToString(task.ID),
	)
	return nil
}
