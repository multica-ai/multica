package wakeup

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	absoluteMaxActiveWakeupsPerAgentIssue = 100
	absoluteMaxWakeupsWithoutProgress     = 50
)

type wakeupCreateFacts struct {
	activeCount                int64
	maxActive                  int
	minInterval                time.Duration
	hasLastFire                bool
	secondsAfterLastFire       int64
	consecutiveWithoutProgress int64
	maxWithoutProgress         int
	triggerEnabled             bool
	progress                   cerebrodb.WakeupProgressCountersRow
}

func (s *Service) beforeWakeupCreate(ctx context.Context, workspaceID pgtype.UUID, req CreateRequest) error {
	if s == nil || s.Hooks == nil {
		return fmt.Errorf("before.wakeup.create Workflow evaluator is unavailable")
	}
	facts, err := s.loadWakeupCreateFacts(ctx, workspaceID, req)
	if err != nil {
		return err
	}
	fireAt := ""
	secondsUntilFire := int64(0)
	if req.FireAt.Valid {
		fireAt = req.FireAt.Time.UTC().Format(time.RFC3339Nano)
		secondsUntilFire = int64(time.Until(req.FireAt.Time).Seconds())
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf(
		"%s:%s:%s:%s:%s:%s:%s:%+v",
		util.UUIDToString(workspaceID), util.UUIDToString(req.AgentID), util.UUIDToString(req.IssueID),
		req.TriggerType, fireAt, util.UUIDToString(req.WatchIssueID), req.WatchStatus.String,
		facts,
	)))
	eventID := fmt.Sprintf("%s:wakeup-create:%x", util.UUIDToString(workspaceID), fingerprint[:12])
	result, err := s.Hooks.Evaluate(ctx, workflows.HookEvent{
		EventID: eventID, Type: workflows.HookBeforeWakeupCreate,
		WorkspaceID: util.UUIDToString(workspaceID), AgentID: util.UUIDToString(req.AgentID),
		IssueID: util.UUIDToString(req.IssueID),
		Proposed: map[string]any{
			"trigger_type": req.TriggerType, "fire_at": fireAt,
			"watch_issue_id": util.UUIDToString(req.WatchIssueID), "watch_status": req.WatchStatus.String,
		},
		Context: map[string]any{
			"wakeup": map[string]any{
				"trigger_type": req.TriggerType, "trigger_enabled": facts.triggerEnabled,
				"active_count": facts.activeCount, "max_active": facts.maxActive,
				"min_interval_seconds": int64(facts.minInterval.Seconds()),
				"seconds_until_fire":   secondsUntilFire,
				"has_last_fire":        facts.hasLastFire, "seconds_after_last_fire": facts.secondsAfterLastFire,
				"loop_limit_enabled":           facts.maxWithoutProgress > 0,
				"consecutive_without_progress": facts.consecutiveWithoutProgress,
				"max_without_progress":         facts.maxWithoutProgress,
				"since_member_reply":           facts.progress.SinceMemberReply,
				"since_status_change":          facts.progress.SinceStatusChange,
				"since_progress_update":        facts.progress.SinceProgressUpdate,
				"since_pull_request_update":    facts.progress.SincePullRequestUpdate,
				"expected_continuation":        "resume_issue_workflow",
			},
		},
	})
	if err != nil {
		return err
	}
	if result.Decision == workflows.HookBlock || result.Decision == workflows.HookRequire {
		requirement := "Workflow blocked wakeup creation."
		if len(result.Requirements) > 0 {
			requirement = strings.Join(result.Requirements, " ")
		}
		return fmt.Errorf("%s", result.ReasonWithHook(requirement))
	}
	return nil
}

func (s *Service) loadWakeupCreateFacts(ctx context.Context, workspaceID pgtype.UUID, req CreateRequest) (wakeupCreateFacts, error) {
	maxActive, minInterval := s.selfWakeupLimits(ctx, workspaceID)
	if minInterval < minWakeupIntervalFloor {
		minInterval = minWakeupIntervalFloor
	}
	facts := wakeupCreateFacts{
		maxActive: maxActive, minInterval: minInterval,
		maxWithoutProgress: s.maxConsecutiveWakeupLoops(ctx, workspaceID),
		triggerEnabled:     s.triggerTypeEnabled(ctx, workspaceID, req.TriggerType),
	}
	var err error
	facts.activeCount, err = s.Cerebro.CountActiveWakeupsForAgentIssue(ctx, cerebrodb.CountActiveWakeupsForAgentIssueParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
	})
	if err != nil {
		return wakeupCreateFacts{}, fmt.Errorf("load active wakeup count: %w", err)
	}
	facts.consecutiveWithoutProgress, err = s.Cerebro.CountConsecutiveSelfWakeupsForAgentIssue(ctx, cerebrodb.CountConsecutiveSelfWakeupsForAgentIssueParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
	})
	if err != nil {
		return wakeupCreateFacts{}, fmt.Errorf("load wakeup progress: %w", err)
	}
	facts.progress, err = s.Cerebro.WakeupProgressCounters(ctx, cerebrodb.WakeupProgressCountersParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
	})
	if err != nil {
		return wakeupCreateFacts{}, fmt.Errorf("load wakeup progress signals: %w", err)
	}
	if req.TriggerType == TriggerTime && req.FireAt.Valid {
		lastFire, loadErr := s.Cerebro.MaxActiveTimeWakeupFireAtForAgentIssue(ctx, cerebrodb.MaxActiveTimeWakeupFireAtForAgentIssueParams{
			WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
		})
		if loadErr != nil {
			return wakeupCreateFacts{}, fmt.Errorf("load latest wakeup time: %w", loadErr)
		}
		facts.hasLastFire = lastFire.Valid
		if lastFire.Valid {
			facts.secondsAfterLastFire = int64(req.FireAt.Time.Sub(lastFire.Time).Seconds())
		}
	}
	return facts, nil
}

func (s *Service) enforceAbsoluteCreateFloors(ctx context.Context, workspaceID pgtype.UUID, req CreateRequest) error {
	if req.TriggerType == TriggerTime && req.FireAt.Time.Before(time.Now().Add(minWakeupIntervalFloor)) {
		return fmt.Errorf("fire_at must be at least %d minute from now", int(minWakeupIntervalFloor.Minutes()))
	}
	active, err := s.Cerebro.CountActiveWakeupsForAgentIssue(ctx, cerebrodb.CountActiveWakeupsForAgentIssueParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
	})
	if err != nil {
		return fmt.Errorf("load active wakeup safety count: %w", err)
	}
	if active >= absoluteMaxActiveWakeupsPerAgentIssue {
		return fmt.Errorf("absolute wakeup safety limit reached")
	}
	consecutive, err := s.Cerebro.CountConsecutiveSelfWakeupsForAgentIssue(ctx, cerebrodb.CountConsecutiveSelfWakeupsForAgentIssueParams{
		WorkspaceID: workspaceID, AgentID: req.AgentID, IssueID: req.IssueID,
	})
	if err != nil {
		return fmt.Errorf("load wakeup progress safety count: %w", err)
	}
	if consecutive >= absoluteMaxWakeupsWithoutProgress {
		return fmt.Errorf("absolute wakeup progress safety limit reached")
	}
	return nil
}

func activeTaskForAgent(tasks []db.AgentTaskQueue, agentID pgtype.UUID) bool {
	for _, task := range tasks {
		if task.AgentID == agentID {
			return true
		}
	}
	return false
}

type wakeupDispatchFailure struct {
	Reason string
	Issue  db.Issue
	Err    error
}

func (f wakeupDispatchFailure) Error() string {
	if f.Err == nil {
		return f.Reason
	}
	return f.Err.Error()
}

type wakeupFireDecision struct {
	Action          string
	PostponeDelay   time.Duration
	NotifyAfter     int32
	WorkflowHandled bool
}

func (s *Service) handleWakeupFireFailure(ctx context.Context, row cerebrodb.CerebroAgentWakeup, failure wakeupDispatchFailure) error {
	decision, err := s.wakeupFireDecision(ctx, row, failure)
	if err != nil {
		return err
	}
	switch decision.Action {
	case "postpone":
		issue := failure.Issue
		if !issue.ID.Valid {
			issue, err = s.Queries.GetIssue(ctx, row.IssueID)
			if err != nil {
				return fmt.Errorf("load issue for wakeup postpone: %w", err)
			}
		}
		delay := decision.PostponeDelay
		if delay < minWakeupIntervalFloor {
			delay = minWakeupIntervalFloor
		}
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		return s.postpone(ctx, row, issue, failure.Reason, delay, decision.NotifyAfter)
	default:
		errMessage := failure.Error()
		slog.Error("cerebro wakeup dispatch failed", "wakeup_id", util.UUIDToString(row.ID), "reason", failure.Reason, "error", failure.Err)
		return s.Cerebro.MarkWakeupFailed(context.Background(), cerebrodb.MarkWakeupFailedParams{
			ID: row.ID, Failure: pgtype.Text{String: truncateFailure(errMessage), Valid: true},
		})
	}
}

func (s *Service) wakeupFireDecision(ctx context.Context, row cerebrodb.CerebroAgentWakeup, failure wakeupDispatchFailure) (wakeupFireDecision, error) {
	safe := wakeupFireDecision{Action: "fail"}
	if s == nil || s.Hooks == nil {
		return safe, nil
	}
	result, err := s.Hooks.Evaluate(ctx, workflows.HookEvent{
		EventID:     fmt.Sprintf("%s:wakeup-fire-failure:%s:%d", util.UUIDToString(row.ID), failure.Reason, row.ConsecutivePostpones+1),
		Type:        workflows.HookOnWakeupFireFailure,
		WorkspaceID: util.UUIDToString(row.WorkspaceID), AgentID: util.UUIDToString(row.AgentID),
		IssueID: util.UUIDToString(row.IssueID), Attempt: int(row.ConsecutivePostpones + 1),
		Context: map[string]any{
			"failure": map[string]any{
				"reason": failure.Reason, "message": failure.Error(),
				"consecutive_postpones":     row.ConsecutivePostpones,
				"next_consecutive_postpone": row.ConsecutivePostpones + 1,
			},
			"wakeup": map[string]any{
				"id": util.UUIDToString(row.ID), "trigger_type": row.TriggerType,
				"expected_continuation": "resume_issue_workflow",
			},
		},
		MutableFields: []string{"failure_action", "postpone_seconds", "notify_after"},
	})
	if err != nil {
		slog.Warn("cerebro wakeup failure hook failed; failing safely", "wakeup_id", util.UUIDToString(row.ID), "error", err)
		return safe, nil
	}
	if !result.Evaluated {
		return safe, nil
	}
	decision := wakeupFireDecision{Action: "fail", WorkflowHandled: true}
	if action, ok := result.Modifications["failure_action"].(string); ok && (action == "fail" || action == "postpone") {
		decision.Action = action
	}
	if seconds, ok := integerHookValue(result.Modifications["postpone_seconds"]); ok && seconds >= 0 {
		decision.PostponeDelay = time.Duration(seconds) * time.Second
	}
	if notifyAfter, ok := integerHookValue(result.Modifications["notify_after"]); ok && notifyAfter >= 0 && notifyAfter <= 2147483647 {
		decision.NotifyAfter = int32(notifyAfter)
	}
	return decision, nil
}

func integerHookValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
